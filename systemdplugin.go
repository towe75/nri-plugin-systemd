/*
   Copyright 2024 Thomas Weber
   Derived from example code Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"
)

var (
	log     *logrus.Logger
	verbose bool
)

type plugin struct {
	stub stub.Stub
}

func (p *plugin) CreateContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	ctrName := containerName(pod, container)

	if verbose {
		dump("CreateContainer", "pod", pod, "container", container)
	}

	if !isSystemdContainer(container) {
		if verbose {
			log.Infof("%s: not a systemd container, skipping", ctrName)
		}
		return nil, nil, nil
	}

	adjust := &api.ContainerAdjustment{}

	if err := configureCgroupMount(adjust, container, ctrName); err != nil {
		return nil, nil, err
	}

	addSystemdTmpfsMounts(adjust, container)

	setSystemdEnvironment(adjust, pod, container)

	if verbose {
		dump(ctrName, "ContainerAdjustment", adjust)
	} else {
		log.Infof("%s: systemd support configured", ctrName)
	}

	return adjust, nil, nil
}

func isSystemdContainer(container *api.Container) bool {
	if len(container.Args) == 0 {
		return false
	}

	cmd := container.Args[0]
	switch cmd {
	case "/sbin/init", "/lib/systemd/systemd", "/usr/lib/systemd/systemd":
		return true
	}

	// TODO: Add annotation-based detection for explicit systemd container marking
	// Example annotations to support in the future:
	// - "io.systemd.container": "true"
	// - "io.kubernetes.cri-o.systemd-cgroup": "true"
	// - "com.microsoft.lcow.systemd": "true"
	//
	// Implementation would check container.Annotations and pod.Annotations
	// for these keys and return true if present with value "true"

	return false
}

func configureCgroupMount(adjust *api.ContainerAdjustment, container *api.Container, ctrName string) error {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		log.Errorf("%s: cgroup filesystem not available at /sys/fs/cgroup - skipping systemd support", ctrName)
		return nil
	}

	var existingMount *api.Mount
	for _, mount := range container.Mounts {
		if mount.Destination == "/sys/fs/cgroup" {
			existingMount = mount
			break
		}
	}

	if existingMount == nil {
		log.Errorf("%s: no existing cgroup mount found - systemd requires /sys/fs/cgroup", ctrName)
		return fmt.Errorf("cgroup mount required for systemd container")
	}

	hasRO := false
	for _, opt := range existingMount.Options {
		if opt == "ro" {
			hasRO = true
			break
		}
	}

	if !hasRO {
		log.Debugf("%s: cgroup mount already has rw, skipping", ctrName)
		return nil
	}

	options := make([]string, 0, len(existingMount.Options))
	for _, opt := range existingMount.Options {
		if opt == "ro" {
			options = append(options, "rw")
		} else {
			options = append(options, opt)
		}
	}

	adjust.RemoveMount("/sys/fs/cgroup")
	adjust.AddMount(&api.Mount{
		Destination: existingMount.Destination,
		Type:        existingMount.Type,
		Source:      existingMount.Source,
		Options:     options,
	})
	log.Debugf("%s: changed cgroup mount from ro to rw", ctrName)

	return nil
}

func addSystemdTmpfsMounts(adjust *api.ContainerAdjustment, container *api.Container) {
	tmpfsMounts := []struct {
		dest string
		mode string
	}{
		{"/run", "mode=755"},
		{"/run/lock", "mode=755"},
		{"/tmp", "mode=1777"},
		{"/var/log/journal", "mode=755"},
	}

	existingMounts := make(map[string]bool)
	for _, mount := range container.Mounts {
		existingMounts[mount.Destination] = true
	}

	for _, m := range tmpfsMounts {
		if existingMounts[m.dest] {
			continue
		}
		adjust.AddMount(&api.Mount{
			Destination: m.dest,
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"rw", "rprivate", "nosuid", "nodev", m.mode},
		})
	}
}

func setSystemdEnvironment(adjust *api.ContainerAdjustment, pod *api.PodSandbox, container *api.Container) {
	adjust.AddEnv("container", "other")

	hasContainerUUID := false
	for _, env := range container.Env {
		if strings.HasPrefix(env, "container_uuid=") {
			hasContainerUUID = true
			break
		}
	}

	if !hasContainerUUID && container.Id != "" {
		adjust.AddEnv("container_uuid", container.Id)
	}

	if pod != nil {
		if uuid, ok := pod.Annotations["io.kubernetes.pod.uid"]; ok {
			if !hasContainerUUID {
				adjust.AddEnv("container_uuid", uuid)
			}
		}
	}
}

func containerName(pod *api.PodSandbox, container *api.Container) string {
	if pod != nil {
		return pod.Name + "/" + container.Name
	}
	return container.Name
}

func dump(args ...interface{}) {
	var (
		prefix string
		idx    int
	)

	if len(args)&0x1 == 1 {
		prefix = args[0].(string)
		idx++
	}

	for ; idx < len(args)-1; idx += 2 {
		tag, obj := args[idx], args[idx+1]
		msg, err := yaml.Marshal(obj)
		if err != nil {
			log.Infof("%s: %s: failed to dump object: %v", prefix, tag, err)
			continue
		}

		if prefix != "" {
			log.Infof("%s: %s:", prefix, tag)
			for _, line := range strings.Split(strings.TrimSpace(string(msg)), "\n") {
				log.Infof("%s:    %s", prefix, line)
			}
		} else {
			log.Infof("%s:", tag)
			for _, line := range strings.Split(strings.TrimSpace(string(msg)), "\n") {
				log.Infof("  %s", line)
			}
		}
	}
}

func main() {
	var (
		pluginIdx  string
		socketPath string
		opts       []stub.Option
		err        error
	)

	log = logrus.StandardLogger()
	log.SetFormatter(&logrus.TextFormatter{
		PadLevelText: true,
	})

	flag.StringVar(&pluginIdx, "idx", "", "plugin index to register to NRI")
	flag.StringVar(&socketPath, "socket-path", "", "path of the NRI socket file")
	flag.BoolVar(&verbose, "verbose", false, "enable (more) verbose logging")
	flag.Parse()

	if pluginIdx != "" {
		opts = append(opts, stub.WithPluginIdx(pluginIdx))
	}

	if socketPath != "" {
		opts = append(opts, stub.WithSocketPath(socketPath))
	}

	if verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	p := &plugin{}
	if p.stub, err = stub.New(p, opts...); err != nil {
		log.Errorf("failed to create plugin stub: %v", err)
		os.Exit(1)
	}

	ctx := context.Background()

	err = p.stub.Run(ctx)
	if err != nil {
		log.Errorf("plugin exited with error %v", err)
		os.Exit(1)
	}
}
