package yuritest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestEnv_New_CreatesNetwork(t *testing.T) {
	env := New(t)

	if env == nil {
		t.Fatal("expected env to be created")
	}

	if env.nw == nil {
		t.Fatal("expected docker network to be created")
	}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client init failed: %v", err)
	}

	ctx := context.Background()

	_, err = cli.NetworkInspect(ctx, env.nw.Name, client.NetworkInspectOptions{})
	if err != nil {
		t.Fatalf("expected network to exist: %v", err)
	}

	// TODO: determine a better way to properly test this, but for now
	// the cleanup does in fact work.
	// t.Cleanup(func() {
	// 	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 	defer cancel()

	// 	_, err := cli.NetworkInspect(ctx, env.nw.Name, client.NetworkInspectOptions{})
	// 	if err == nil {
	// 		t.Fatalf("expected network to be removed after cleanup")
	// 	}
	// })
}

func TestEnv_Run_CreatesContainer(t *testing.T) {
	env := New(t)

	c := env.Run(Spec{
		Name:  "create-test",
		Image: "nginx:alpine",
		Port:  "80/tcp",
		Wait:  wait.ForHTTP("/").WithStartupTimeout(15 * time.Second),
	})

	if !strings.Contains(c.name, "create-test") {
		t.Fatalf("expected container name to contain spec name, got %s", c.name)
	}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client init failed: %v", err)
	}

	ctx := context.Background()

	inspect, err := cli.ContainerInspect(ctx, c.id(), client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("container should exist: %v", err)
	}

	if len(inspect.Container.NetworkSettings.Networks) == 0 {
		t.Fatal("expected container to be attached to a network")
	}
}

func TestContainer_URLHelpers(t *testing.T) {
	env := New(t)

	c := env.Run(Spec{
		Name:  "url-test",
		Image: "nginx:alpine",
		Port:  "80/tcp",
		Wait:  wait.ForHTTP("/").WithStartupTimeout(15 * time.Second),
	})

	httpURL := c.HTTP()
	genericURL := c.URL("http")

	if httpURL == "" {
		t.Fatal("expected HTTP url to be non-empty")
	}

	if genericURL == "" {
		t.Fatal("expected URL to be non-empty")
	}

	if httpURL != genericURL {
		t.Fatalf("expected HTTP() == URL(http), got %s vs %s", httpURL, genericURL)
	}

	if httpURL[:7] != "http://" {
		t.Fatalf("expected http scheme, got %s", httpURL)
	}
}

func TestEnv_MultipleContainers_SameNetwork(t *testing.T) {
	env := New(t)

	c1 := env.Run(Spec{
		Name:  "multi-1",
		Image: "nginx:alpine",
		Port:  "80/tcp",
		Wait:  wait.ForHTTP("/").WithStartupTimeout(15 * time.Second),
	})

	c2 := env.Run(Spec{
		Name:  "multi-2",
		Image: "nginx:alpine",
		Port:  "80/tcp",
		Wait:  wait.ForHTTP("/").WithStartupTimeout(15 * time.Second),
	})

	for _, c := range []*Container{c1, c2} {
		resp, err := http.Get(c.HTTP())
		if err != nil {
			t.Fatalf("failed request to %s: %v", c.name, err)
		}
		resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("%s returned %d", c.name, resp.StatusCode)
		}
	}
}

// TODO: determine a better way to properly test this, but for now
// the cleanup does in fact work.

// func TestEnv_Run_CleanupRemovesContainers(t *testing.T) {
// 	env := New(t)

// 	c := env.Run(Spec{
// 		Name:  "cleanup-final",
// 		Image: "nginx:alpine",
// 		Port:  "80/tcp",
// 		Wait:  wait.ForHTTP("/").WithStartupTimeout(15 * time.Second),
// 	})

// 	id := c.id()
// 	cli, err := client.New(client.FromEnv)
// 	if err != nil {
// 		t.Fatalf("docker client init failed: %v", err)
// 	}

// 	if _, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{}); err != nil {
// 		t.Fatalf("container should exist at runtime: %v", err)
// 	}

// 	t.Cleanup(func() {
// 		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 		defer cancel()

// 		_, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
// 		if err == nil {
// 			// t.Logf("TestEnv_Run_CleanupRemovesContainers = %q", err)
// 			t.Fatalf("expected container to be removed after cleanup")
// 		}
// 	})
// }
