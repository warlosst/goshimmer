package framework

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// newDockerClient creates a Docker client that communicates via the Docker socket.
func newDockerClient() (*client.Client, error) {
	return client.NewClient(
		"unix:///var/run/docker.sock",
		"",
		nil,
		nil,
	)
}

// DockerContainer is a wrapper object for a Docker container.
type DockerContainer struct {
	client *client.Client
	id     string
}

// NewDockerContainer creates a new DockerContainer.
func NewDockerContainer(c *client.Client) *DockerContainer {
	return &DockerContainer{client: c}
}

// NewDockerContainerFromExisting creates a new DockerContainer from an already existing Docker container by name.
func NewDockerContainerFromExisting(c *client.Client, name string) (*DockerContainer, error) {
	containers, err := c.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return nil, err
	}

	for _, cont := range containers {
		if cont.Names[0] == name {
			return &DockerContainer{
				client: c,
				id:     cont.ID,
			}, nil
		}
	}

	return nil, fmt.Errorf("could not find container with name '%s'", name)
}

// CreateGoShimmerEntryNode creates a new container with the GoShimmer entry node's configuration.
func (d *DockerContainer) CreateGoShimmerEntryNode(name string, seed string) error {
	containerConfig := &container.Config{
		Image:        "iotaledger/goshimmer",
		ExposedPorts: nil,
		Cmd: strslice.StrSlice{
			"--skip-config=true",
			"--logger.level=debug",
			fmt.Sprintf("--node.disablePlugins=%s", disabledPluginsEntryNode),
			"--autopeering.entryNodes=",
			fmt.Sprintf("--autopeering.seed=%s", seed),
		},
	}

	return d.CreateContainer(name, containerConfig)
}

// CreateGoShimmerPeer creates a new container with the GoShimmer peer's configuration.
func (d *DockerContainer) CreateGoShimmerPeer(config GoShimmerConfig) error {
	// configure GoShimmer container instance
	containerConfig := &container.Config{
		Image: "iotaledger/goshimmer",
		ExposedPorts: nat.PortSet{
			nat.Port("8080/tcp"): {},
		},
		Cmd: strslice.StrSlice{
			"--skip-config=true",
			"--logger.level=debug",
			fmt.Sprintf("--node.disablePlugins=%s", config.DisabledPlugins),
			fmt.Sprintf("--node.enablePlugins=%s", func() string {
				if config.Bootstrap {
					return "Bootstrap"
				}
				return ""
			}()),
			fmt.Sprintf("--bootstrap.initialIssuance.timePeriodSec=%d", config.BootstrapInitialIssuanceTimePeriodSec),
			"--webapi.bindAddress=0.0.0.0:8080",
			fmt.Sprintf("--autopeering.seed=%s", config.Seed),
			fmt.Sprintf("--autopeering.entryNodes=%s@%s:14626", config.EntryNodePublicKey, config.EntryNodeHost),
			fmt.Sprintf("--drng.instanceId=%d", config.DRNGInstance),
			fmt.Sprintf("--drng.threshold=%d", config.DRNGThreshold),
			fmt.Sprintf("--drng.committeeMembers=%s", config.DRNGCommittee),
			fmt.Sprintf("--drng.distributedPubKey=%s", config.DRNGDistKey),
		},
	}

	return d.CreateContainer(config.Name, containerConfig)
}

// CreateDrandMember creates a new container with the drand configuration.
func (d *DockerContainer) CreateDrandMember(name string, goShimmerAPI string, leader bool) error {
	// configure drand container instance
	env := []string{}
	if leader {
		env = append(env, "LEADER=1")
	}
	env = append(env, "GOSHIMMER=http://"+goShimmerAPI)
	containerConfig := &container.Config{
		Image: "angelocapossele/drand:latest",
		ExposedPorts: nat.PortSet{
			nat.Port("8000/tcp"): {},
		},
		Env:        env,
		Entrypoint: strslice.StrSlice{"/data/client-script.sh"},
	}

	return d.CreateContainer(name, containerConfig)
}

// CreateContainer creates a new container with the given configuration.
func (d *DockerContainer) CreateContainer(name string, containerConfig *container.Config) error {
	resp, err := d.client.ContainerCreate(context.Background(), containerConfig, nil, nil, name)
	if err != nil {
		return err
	}

	d.id = resp.ID
	return nil
}

// ConnectToNetwork connects a container to an existent network in the docker host.
func (d *DockerContainer) ConnectToNetwork(networkID string) error {
	return d.client.NetworkConnect(context.Background(), networkID, d.id, nil)
}

// DisconnectFromNetwork disconnects a container from an existent network in the docker host.
func (d *DockerContainer) DisconnectFromNetwork(networkID string) error {
	return d.client.NetworkDisconnect(context.Background(), networkID, d.id, true)
}

// Start sends a request to the docker daemon to start a container.
func (d *DockerContainer) Start() error {
	return d.client.ContainerStart(context.Background(), d.id, types.ContainerStartOptions{})
}

// Remove kills and removes a container from the docker host.
func (d *DockerContainer) Remove() error {
	return d.client.ContainerRemove(context.Background(), d.id, types.ContainerRemoveOptions{Force: true})
}

// Stop stops a container without terminating the process.
// The process is blocked until the container stops or the timeout expires.
func (d *DockerContainer) Stop() error {
	duration := 30 * time.Second
	return d.client.ContainerStop(context.Background(), d.id, &duration)
}

// ExitStatus returns the exit status according to the container information.
func (d *DockerContainer) ExitStatus() (int, error) {
	resp, err := d.client.ContainerInspect(context.Background(), d.id)
	if err != nil {
		return -1, err
	}

	return resp.State.ExitCode, nil
}

// Logs returns the logs of the container as io.ReadCloser.
func (d *DockerContainer) Logs() (io.ReadCloser, error) {
	options := types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Since:      "",
		Timestamps: false,
		Follow:     false,
		Tail:       "",
		Details:    false,
	}

	return d.client.ContainerLogs(context.Background(), d.id, options)
}
