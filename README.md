# Systemd NRI Plugin

An [NRI](https://github.com/containerd/nri) (Node Resource Interface) plugin that enables systemd containers to run in Kubernetes by modifying the container runtime spec before container creation.

## Overview

This plugin prepares containers running systemd as PID 1 by:

1. **Making cgroups writable**: Changes the `/sys/fs/cgroup` mount from read-only to read-write, which systemd requires to manage services
2. **Adding tmpfs mounts**: Creates necessary tmpfs mounts for `/run`, `/run/lock`, `/tmp`, and `/var/log/journal` if they don't already exist
3. **Setting environment variables**: Sets `container=other` and `container_uuid` for systemd container detection and machine-id generation

Having RW cgroups with secure mount delegation enables:

- **Docker/Podman in Kubernetes**: Run container runtimes inside Kubernetes pods for CI/CD pipelines, testing environments, or development sandboxes
- **Kubernetes in Kubernetes**: Self-hosted Kubernetes clusters, nested testing environments, and control plane experimentation (note: this use case is not yet tested and may require additional refinements)

Running systemd in Kubernetes enables:

- **Legacy application migration**: Move existing multi-service applications from VMs to containers without complex refactoring into microservices ("Lift-and-shift")
- **Process management**: Leverage systemd's robust service supervision, automatic restarts, and dependency management
- **Unit file compatibility**: Use existing, well-tested systemd unit files instead of custom init scripts
- **Socket activation**: Defer service startup until connections arrive, reducing resource consumption and enabling on-demand activation
- **Timers**: Replace cron jobs with systemd timers for scheduled tasks, with better logging, dependency management, and execution tracking

This plugin makes systemd containers work in Kubernetes **without requiring privileged mode** or runtime-specific configuration.

## Roadmap

- ✅ Initial POC implementation
- 🔄 CI/CD with GitHub Actions and container image builds
- 🔄 Kustomize support or alternative auto-install solution
- 🔄 Opt-in/opt-out via annotations (e.g., `io.systemd.container=true`)
- 🔄 Configurable cgroup RW via annotation (independent of systemd entrypoint detection)
- 🔄 SELinux and AppArmor integration for nested container scenarios (podman-in-kubernetes, docker-in-kubernetes, kubernetes-in-kubernetes)
- 🔄 Automatic stop signal injection (`SIGRTMIN+3`) for systemd containers

## Background & History

Running systemd inside containers has been a long-standing challenge. Daniel Walsh from Red Hat has documented this evolution since 2014, starting with his original article ["Running systemd within a Docker Container"](https://developers.redhat.com/blog/2014/05/05/running-systemd-within-docker-container/), noting that early Docker and systemd communities were initially hostile to the idea. He continued updating this guidance, with the [most recent 2019 article](https://developers.redhat.com/blog/2019/04/24/how-to-run-systemd-in-a-container) reflecting the current state.

**Key milestones:**

- **oci-systemd-hook** (2016): Walsh implemented the [oci-systemd-hook](https://github.com/projectatomic/oci-systemd-hook) project (GPL-3.0) to provide OCI hooks for enabling systemd containers in runc/Docker

- **Podman** (since 2018): Red Hat's Podman container engine added native systemd support, automatically configuring tmpfs mounts and cgroups when detecting systemd/init as the container entrypoint

- **nomad-driver-podman** (since 2019): The author's previous work, [nomad-driver-podman](https://github.com/hashicorp/nomad-driver-podman), enabled HashiCorp Nomad to run systemd containers via Podman's API

- **systemd.io/CONTAINER_INTERFACE** (2020): The systemd project published official [container interface specifications](https://systemd.io/CONTAINER_INTERFACE/) documenting what systemd needs to run in containers (writable cgroups, tmpfs mounts, environment variables)

- **NRI (Node Resource Interface)** (2021): The containerd project published the initial [NRI v0.1.0 release](https://github.com/containerd/nri/releases/tag/v0.1.0) (April 2021), providing a runtime-agnostic plugin framework for modifying container specs

- **NRI adoption in runtimes** (2022-2023): containerd v1.7+ and CRI-O v1.26+ added NRI support, making it available for Kubernetes workloads


## How It Works

### Cgroup v2 mount

With cgroup v2 unified hierarchy, `/sys/fs/cgroup` is mounted read-only by default for unprivileged containers. This broke systemd containers in Kubernetes without workarounds:

- **containerd**: Added `cgroup_writable` configuration option (issue [#10924](https://github.com/containerd/containerd/issues/10924), PR [dda7020](https://github.com/containerd/containerd/commit/dda7020))
- **CRI-O**: Supports annotation `io.kubernetes.cri-o.cgroup2-mount-hierarchy-rw`

**Before NRI**, enabling writable cgroups required runtime-specific configuration or privileged containers. NRI provides a clean, runtime-agnostic plugin mechanism to modify container specs before creation.

**Security considerations:** Making cgroups writable raises security questions about container isolation. The containerd implementation (PR [#11131](https://github.com/containerd/containerd/pull/11131)) addressed this through configuration-based enablement with cgroup v2-only restrictions. The key insight is that cgroup v2's delegated mount model allows containers to manage their own sub-hierarchies without affecting the host or other containers. When properly configured with user namespaces and cgroup controllers, writable cgroups provide the necessary systemd functionality while maintaining security boundaries. This plugin follows the same principle by modifying only the container's own cgroup mount, not the host's cgroup hierarchy.


The plugin preserves all existing cgroup mount attributes and only modifies the mount if it's read-only:
- Detects the existing cgroup mount type (`cgroup` or `cgroup2`)
- Replaces `ro` option with `rw` while keeping all other options intact
- Returns an error if no cgroup mount exists (systemd requires cgroups)

### Systemd Detection

The plugin automatically detects systemd containers by checking the container's entrypoint/command:
- `/sbin/init`
- `/lib/systemd/systemd`
- `/usr/lib/systemd/systemd`

Future versions may support annotation-based opt-in or opt-out.

### Machine-ID Generation

The plugin sets the `container_uuid` environment variable to enable systemd's automatic machine-id generation:
1. Uses the container's unique ID if available
2. Falls back to the pod UID from Kubernetes annotations
3. But keep user defined `container_uuid` env if already set in the container spec

According to [systemd documentation](https://www.freedesktop.org/software/systemd/man/latest/systemd.html), when `/etc/machine-id` is empty at boot time, systemd will use the `container_uuid` environment variable to initialize it automatically.

## Building

```bash
go build -o nri-plugin-systemd .
```

## Deployment

### Direct Execution

Run the plugin directly on the target node:

```bash
./nri-plugin-systemd -idx 10 -verbose
```

The `-idx` flag specifies the plugin invocation order (lower numbers run first).

### As a Kubernetes DaemonSet

Create a DaemonSet to deploy the plugin across all nodes:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: systemd-nri-plugin
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: systemd-nri-plugin
  template:
    metadata:
      labels:
        app: systemd-nri-plugin
    spec:
      hostNetwork: true
      containers:
      - name: nri-plugin-systemd
        image: your-registry/nri-plugin-systemd:latest
        args:
        - -idx=10
        - -verbose
        volumeMounts:
        - name: nri-socket
          mountPath: /var/run/nri
      volumes:
      - name: nri-socket
        hostPath:
          path: /var/run/nri
```

## Configuration

The plugin supports the following command-line flags:

- `-idx <string>`: Plugin index for NRI invocation order (required)
- `-socket-path <string>`: Path to the NRI socket (default: `/var/run/nri/nri.sock`)
- `-verbose`: Enable verbose logging

## Requirements

- Container runtime with NRI support enabled
  - **containerd v2.0+**: NRI enabled by default (no configuration needed)
  - **containerd v1.7+**: NRI available but may require explicit enablement
  - **CRI-O v1.26+**: NRI support available
- Cgroup filesystem available at `/sys/fs/cgroup`
- Containers must have a cgroup mount configured

> **Note:** For containerd v2.0+, NRI is enabled by default. No additional configuration is required. For older versions, ensure NRI is enabled in your containerd configuration (`/etc/containerd/config.toml`). See the [containerd NRI documentation](https://github.com/containerd/containerd/blob/main/docs/NRI.md) for details.

## Testing

Test with a systemd-enabled container:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: systemd-test
spec:
  containers:
  - name: systemd
    image: nestybox/ubuntu-noble-systemd:latest
    command: ["/lib/systemd/systemd"]
    args: ["--system", "--unit=multi-user.target", "--log-target=console", "--log-level=debug"]
    tty: true
    lifecycle:
      stopSignal: "SIGRTMIN+3"
```

For reference, see the [nestybox ubuntu-noble-systemd Dockerfile](https://github.com/nestybox/dockerfiles/blob/master/ubuntu-noble-systemd/Dockerfile) which demonstrates proper systemd container configuration including the `STOPSIGNAL` directive.

Verify the container starts successfully and systemd is running as PID 1:

```bash
kubectl logs systemd-test 
kubectl exec systemd-test -- ps aux
kubectl exec systemd-test -- systemctl status
```

## Stop Signal Configuration

Systemd requires `SIGRTMIN+3` (signal 37) for clean shutdown, not the default `SIGTERM`. Without this signal, systemd containers may not shut down gracefully, causing:
- Delayed pod termination (waiting for timeout)
- Services not stopping in the correct order
- Potential data loss from abrupt termination

### Configuration Options

**Option 1: Kubernetes Manifest** (Recommended for testing)

Add the `stopSignal` field to your container spec:

```yaml
spec:
  containers:
  - name: systemd
    image: your-image
    lifecycle:
      stopSignal: "SIGRTMIN+3"
```

**Option 2: Container Image (Dockerfile)**

Set the stop signal in your Dockerfile:

```dockerfile
FROM ubuntu:noble

# ... install systemd ...

STOPSIGNAL SIGRTMIN+3

CMD ["/lib/systemd/systemd"]
```

**Option 3: Runtime-Specific Annotation**

For automated deployment, use runtime annotations:

```yaml
# containerd
annotations:
  io.kubernetes.cri.container-stop-signal: "SIGRTMIN+3"

# CRI-O
annotations:
  io.kubernetes.cri-o.StopSignal: "SIGRTMIN+3"
```

## Troubleshooting

### Container fails with "no existing cgroup mount found"

The container must have a cgroup mount configured. Ensure your runtime is configured to mount cgroups.

### Enable systemd startup debug output

See above:

```
...
    command: ["/lib/systemd/systemd"]
    args: ["--system", "--unit=multi-user.target", "--log-target=console", "--log-level=debug"]
    tty: true
...
```

Follow startup/shutdown logs:

```
kubectl logs systemd-test -f
```

### Verbose logging

Enable verbose mode to see detailed plugin operations:

```bash
./nri-plugin-systemd -idx 10 -verbose
```

## Related Projects & References

- [systemd Container Interface Specification](https://systemd.io/CONTAINER_INTERFACE/)
- [oci-systemd-hook](https://github.com/projectatomic/oci-systemd-hook) - OCI hooks for systemd containers
- [How to run systemd in a container](https://developers.redhat.com/blog/2019/04/24/how-to-run-systemd-in-a-container) - Red Hat blog post by Daniel Walsh (2019)
- [containerd cgroup_writable feature](https://github.com/containerd/containerd/issues/10924) - Issue and implementation discussion
- [nomad-driver-podman](https://github.com/hashicorp/nomad-driver-podman) - HashiCorp Nomad driver for Podman (author's previous work)
- [Podman documentation](https://podman.io/) - Container engine with native systemd support

## License

Copyright 2026 Thomas Weber
Derived from example code Copyright The containerd Authors.

Licensed under the Apache License, Version 2.0.
