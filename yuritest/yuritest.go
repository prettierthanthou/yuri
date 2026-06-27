package yuritest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

type Env struct {
	t   *testing.T
	ctx context.Context
	nw  *testcontainers.DockerNetwork
}

type Spec struct {
	Name       string
	Image      string
	Entrypoint []string
	Cmd        []string
	Env        map[string]string

	Port   string
	Mounts []testcontainers.ContainerMount

	Wait wait.Strategy
}

type Container struct {
	c    testcontainers.Container
	host string
	port string
	name string
}

// New creates a new container env.
// An Env should not be shared across multiple tests, while this might
// use more resources and result in longer testing durations this prevents
// global mutations which are extremely common for daemons for cryptocurrencies.
func New(t *testing.T) *Env {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	nw, err := network.New(ctx)
	if err != nil {
		t.Skipf("testcontainers network unavailable: %v", err)
	}

	e := &Env{
		t:   t,
		ctx: ctx,
		nw:  nw,
	}

	t.Cleanup(func() {
		done := make(chan struct{})

		go func() {
			_ = nw.Remove(context.Background())
			close(done)
		}()

		select {
		case <-done:
			return
		case <-time.After(10 * time.Second):
			t.Fatalf("network cleanup timed out after 10s")
		}
	})

	return e
}

func (e *Env) Run(spec Spec) *Container {
	e.t.Helper()

	containerName := fmt.Sprintf("yuri-%s", spec.Name)
	e.t.Logf("creating %s container", containerName)
	req := testcontainers.ContainerRequest{
		Name:         containerName,
		Image:        spec.Image,
		Entrypoint:   spec.Entrypoint,
		Cmd:          spec.Cmd,
		Env:          spec.Env,
		ExposedPorts: []string{spec.Port},
		Networks:     []string{e.nw.Name},
		Mounts:       spec.Mounts,
		WaitingFor:   spec.Wait,
	}

	c, err := testcontainers.GenericContainer(e.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		e.t.Fatalf("failed to start container %s: %v", spec.Name, err)
	}

	host, err := c.Host(e.ctx)
	if err != nil {
		e.t.Fatalf("host lookup failed for %s: %v", spec.Name, err)
	}

	mapped, err := c.MappedPort(e.ctx, spec.Port)
	if err != nil {
		e.t.Fatalf("port mapping failed for %s: %v", spec.Name, err)
	}

	container := &Container{
		c:    c,
		host: host,
		port: mapped.Port(),
		name: containerName,
	}

	e.t.Cleanup(func() {
		done := make(chan struct{})

		go func() {
			_ = c.Terminate(e.ctx)
			close(done)
		}()

		select {
		case <-done:
			return
		case <-time.After(15 * time.Second):
			e.t.Fatalf("container %s did not terminate within 10s", spec.Name)
		}
	})

	return container
}

// id gets the container ID, this is used
// internally for testing cleanup works
func (c *Container) id() string {
	return c.c.GetContainerID()
}

func (c *Container) Name() string {
	return c.name
}

func (c *Container) HTTP() string {
	return c.URL("http")
}

func (c *Container) URL(scheme string) string {
	return fmt.Sprintf("%s://%s:%s", scheme, c.host, c.port)
}
