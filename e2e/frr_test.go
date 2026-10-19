// Package e2e contains end-to-end tests that run a real anycastd binary
// (built with GoBGP v4) against an independent BGP peer (FRR) on a docker
// internal network, proving the whole announce/withdraw path works against a
// third-party implementation rather than just the unit-test mocks.
package e2e

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/require"

	docker "github.com/teran/go-docker-testsuite"
	"github.com/teran/go-docker-testsuite/applications/frr"
)

const (
	// announcePrefix is the prefix anycastd announces to the FRR peer.
	announcePrefix = "192.0.2.0/24"
	// anycastASN is the autonomous system of the anycastd binary.
	anycastASN = 65000
	// frrASN is the autonomous system of the FRR peer.
	frrASN = 65001

	anycastdName = "anycastd"
	probeName    = "probe"

	// probeImage is a tiny image that runs an HTTP server the anycastd check
	// points at. Stopping the probe makes the check fail, which drives the
	// service down and triggers a BGP withdrawal (DeletePath) — the critical
	// seam this test exercises.
	probeImage = "index.docker.io/library/alpine:3.20"

	// imageRef is the (unprefixed) name the built anycastd image is tagged
	// with. It is passed unprefixed to the go-docker-testsuite wrapper, which
	// applies IMAGE_PREFIX itself; the image is tagged with the prefixed name
	// so the wrapper finds it locally and skips a bogus pull.
	imageRef = "anycastd-e2e:test"
)

// TestAnycastdAnnouncesAndWithdraws is the e2e seam for the GoBGP v3->v4
// migration: it builds a real anycastd binary (with GoBGP v4), runs it on an
// internal docker network next to an independent FRR BGP peer, and asserts:
//
//  1. anycastd announces the prefix and FRR receives it;
//  2. when the underlying check fails, anycastd withdraws the prefix and FRR
//     observes the withdrawal (this exercises announcer.DeletePath — the path
//     that was broken on v3 and had to be fixed for v4 by flagging the path as
//     a Withdrawal);
//  3. once the anycastd container is stopped the prefix stays withdrawn.
func TestAnycastdAnnouncesAndWithdraws(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)

	// ---- build the anycastd image ----
	imgTag := buildAnycastdImage(t, ctx, cli)

	// ---- construct the group containers ----
	// FRR: no static neighbor (added dynamically after the group is up), and
	// `no bgp ebgp-requires-policy` so FRR 9/10.x actually exchanges prefixes
	// with the eBGP peer instead of holding it in "Policy" with zero routes.
	frrCfg := []byte(fmt.Sprintf(`hostname frr
frr defaults traditional
!
router bgp %d
 no bgp ebgp-requires-policy
 bgp router-id 10.0.0.2
!
`, frrASN))

	frrC, err := frr.NewContainer(ctx, frrCfg, frr.WithContainerName("frr"))
	require.NoError(t, err)

	// anycastd: startup wrapper waits for /config.yaml to appear (it is
	// injected once the container IPs are known), then exec's anycastd so the
	// binary is PID1 and receives a clean SIGTERM on ContainerStop.
	anycastdC, err := docker.NewContainerWithLifecycle(
		anycastdName,
		imgTag,
		[]string{"sh", "-c", "while [ ! -f /config.yaml ]; do sleep 0.2; done; exec /anycastd"},
		docker.NewEnvironment().StringVar("LOG_LEVEL", "DEBUG"),
		docker.NewPortBindings(),
	)
	require.NoError(t, err)

	// probe: a tiny HTTP server the anycastd check points at. Stopping this
	// container drives the check down → withdrawal.
	probeC, err := docker.NewContainer(
		probeName,
		probeImage,
		// alpine:3.20's busybox does not ship the `httpd` applet (it lives in
		// the busybox-extras package), so install it before starting the server.
		// The container also keeps its bridge default route, so apk can reach
		// the network even though the group network is internal.
		[]string{"sh", "-c", "apk add --no-cache busybox-extras >/dev/null 2>&1; mkdir -p /www && echo ok > /www/index.html && exec httpd -f -p 8080 -h /www"},
		docker.NewEnvironment(),
		docker.NewPortBindings(),
	)
	require.NoError(t, err)

	g, err := docker.NewGroup("anycastd-e2e",
		docker.NewApplication(frrC),
		docker.NewApplication(anycastdC),
		docker.NewApplication(probeC),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close(context.Background()) })

	require.NoError(t, g.Run(ctx))

	frrApp := frr.NewFromContainer(frrC)

	// ---- resolve container IPs on the group network ----
	anycastdIP := containerIPv4(ctx, t, cli, anycastdC)
	frrIP := containerIPv4(ctx, t, cli, frrC)
	probeIP := containerIPv4(ctx, t, cli, probeC)

	// ---- inject the anycastd config now that IPs are known ----
	cfg := []byte(fmt.Sprintf(`announcer:
  router_id: 10.0.0.1
  local_address: %s
  local_asn: %d
  routes:
    - %s
  peers:
    - name: frr
      remote_address: %s
      remote_asn: %d
services:
  - name: anycastd-e2e
    check_interval: 500ms
    strategy: at_least_one
    checks:
      - kind: http_2xx
        spec:
          url: http://%s:8080/
          method: GET
          tries: 1
          interval: 100ms
          timeout: 1s
        group: default
metrics:
  enabled: true
  address: :9090
`, anycastdIP, anycastASN, announcePrefix, frrIP, frrASN, probeIP))

	copyConfig(ctx, t, cli, anycastdC, cfg)

	// ---- point FRR at the anycastd peer (retried: bgpd may still be
	// starting right after the group's Run) ----
	addFRRNeighbor(ctx, t, frrApp, anycastdIP)

	// 1. the prefix is announced and FRR receives it.
	require.NoError(t, frrApp.WaitForBGPRoute(ctx, announcePrefix))

	// 2. stop the probe → the http_2xx check fails → anycastd Denounce →
	//    DeletePath → FRR must observe the withdrawal. This is the assertion
	//    that fails (route stays) if DeletePath is not flagged Withdrawal.
	require.NoError(t, cli.ContainerStop(ctx, probeC.ID(), container.StopOptions{}))
	require.NoError(t, frrApp.WaitForBGPRouteWithdrawn(ctx, announcePrefix))

	// 3. stopping the anycastd container keeps the route withdrawn.
	require.NoError(t, cli.ContainerStop(ctx, anycastdC.ID(), container.StopOptions{}))
	require.NoError(t, frrApp.WaitForBGPRouteWithdrawn(ctx, announcePrefix))
}

// buildAnycastdImage compiles a static linux anycastd binary and builds+tags a
// tiny alpine image containing it. It returns the (unprefixed) image reference
// to pass to the go-docker-testsuite wrapper.
func buildAnycastdImage(t *testing.T, ctx context.Context, cli *client.Client) string {
	t.Helper()

	arch := "amd64"
	if info, err := cli.Info(ctx); err == nil && strings.Contains(strings.ToLower(info.Architecture), "arm") {
		arch = "arm64"
	}

	tmp := t.TempDir()

	// static binary for the target docker architecture
	bin := filepath.Join(tmp, "anycastd")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/anycastd")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building anycastd binary: %s", out)

	binData, err := os.ReadFile(bin)
	require.NoError(t, err)

	// tag the image with the prefixed name so the wrapper (which prefixes refs
	// when IMAGE_PREFIX is set) finds it locally and does not attempt a pull;
	// the ref handed to the wrapper stays unprefixed.
	prefix := strings.TrimRight(os.Getenv("IMAGE_PREFIX"), "/")
	tag := imageRef
	if prefix != "" {
		tag = prefix + "/" + imageRef
	}

	dockerfile := "FROM alpine:3.20\nCOPY anycastd /anycastd\n"
	tarReader, err := tarOf(map[string]tarEntry{
		"Dockerfile": {data: []byte(dockerfile), mode: 0644},
		"anycastd":   {data: binData, mode: 0755},
	})
	require.NoError(t, err)

	resp, err := cli.ImageBuild(ctx, tarReader, build.ImageBuildOptions{
		Tags:       []string{tag},
		Remove:     true,
		Dockerfile: "Dockerfile",
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // drain until the build completes

	return imageRef
}

// addFRRNeighbor configures FRR to peer with anycastdIP, retrying until bgpd
// accepts it (it may still be starting after the group's Run).
func addFRRNeighbor(ctx context.Context, t *testing.T, frrApp frr.FRR, anycastdIP string) {
	t.Helper()

	cmd := fmt.Sprintf("configure terminal\nrouter bgp %d\nneighbor %s remote-as %d\nend", frrASN, anycastdIP, anycastASN)
	for {
		_, err := frrApp.ExecVTY(ctx, cmd)
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			require.NoError(t, err)
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// containerIPv4 returns the container's IPv4 on the internal group network
// (i.e. the non-"bridge" network endpoint).
func containerIPv4(ctx context.Context, t *testing.T, cli *client.Client, c docker.Container) string {
	t.Helper()

	insp, err := cli.ContainerInspect(ctx, c.ID())
	require.NoError(t, err)
	require.NotNil(t, insp.NetworkSettings, "container %s has no network settings", c.Name())

	ip := ""
	for name, ep := range insp.NetworkSettings.Networks {
		if strings.EqualFold(name, "bridge") {
			continue
		}
		if ep == nil || ep.IPAddress == "" {
			continue
		}
		if parsed := net.ParseIP(ep.IPAddress); parsed != nil && parsed.To4() != nil {
			ip = ep.IPAddress
			break
		}
	}

	require.NotEmpty(t, ip, "no non-bridge IPv4 address found for container %s", c.Name())
	return ip
}

// copyConfig injects the anycastd config into /config.yaml of the given
// running container via the docker SDK CopyToContainer.
func copyConfig(ctx context.Context, t *testing.T, cli *client.Client, c docker.Container, config []byte) {
	t.Helper()

	tr, err := tarOf(map[string]tarEntry{
		"config.yaml": {data: config, mode: 0644},
	})
	require.NoError(t, err)
	require.NoError(t, cli.CopyToContainer(ctx, c.ID(), "/", tr, container.CopyToContainerOptions{}))
}

type tarEntry struct {
	data []byte
	mode int64
}

// tarOf packs the given files into a single uncompressed tar stream.
func tarOf(files map[string]tarEntry) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, f := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: f.mode,
			Size: int64(len(f.data)),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

// repoRoot locates the anycastd module root by walking up from this file until
// it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	require.FailNow(t, "could not locate module root (go.mod)")
	return ""
}
