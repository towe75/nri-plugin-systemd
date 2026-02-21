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
	"testing"

	"github.com/containerd/nri/pkg/api"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestSystemdPlugin(t *testing.T) {
	log = logrus.StandardLogger()
	log.SetFormatter(&logrus.TextFormatter{PadLevelText: true})

	t.Run("non-systemd container ignored", func(t *testing.T) {
		testNonSystemdContainerIgnored(t)
	})

	t.Run("systemd container detected by /sbin/init", func(t *testing.T) {
		testSystemdContainerDetectedSbinInit(t)
	})

	t.Run("systemd container detected by /lib/systemd/systemd", func(t *testing.T) {
		testSystemdContainerDetectedLibSystemd(t)
	})

	t.Run("systemd container detected by /usr/lib/systemd/systemd", func(t *testing.T) {
		testSystemdContainerDetectedUsrLibSystemd(t)
	})
}

func testNonSystemdContainerIgnored(t *testing.T) {
	t.Helper()

	p := &plugin{}
	pod := &api.PodSandbox{
		Name:        "test-pod",
		Annotations: map[string]string{},
	}
	container := &api.Container{
		Name:        "test-container",
		Annotations: map[string]string{},
		Args:        []string{"/bin/bash"},
		Linux:       &api.LinuxContainer{},
	}

	adjust, updates, err := p.CreateContainer(context.Background(), pod, container)

	assert.NoError(t, err)
	assert.Nil(t, adjust)
	assert.Nil(t, updates)
}

func testSystemdContainerDetectedSbinInit(t *testing.T) {
	t.Helper()

	p := &plugin{}
	pod := &api.PodSandbox{
		Name:        "test-pod-systemd",
		Annotations: map[string]string{},
	}
	container := &api.Container{
		Name:        "test-container-systemd",
		Annotations: map[string]string{},
		Args:        []string{"/sbin/init"},
		Linux:       &api.LinuxContainer{},
		Mounts: []*api.Mount{
			{
				Destination: "/sys/fs/cgroup",
				Type:        "cgroup",
				Source:      "cgroup",
				Options:     []string{"nosuid", "noexec", "nodev", "relatime", "rw"},
			},
		},
		Env: []string{"PATH=/usr/bin"},
		Id:  "test-container-id-12345",
	}

	adjust, updates, err := p.CreateContainer(context.Background(), pod, container)

	assert.NoError(t, err)
	assert.NotNil(t, adjust)
	assert.Nil(t, updates)

	assert.NotNil(t, adjust.Mounts)
	assert.NotNil(t, adjust.Env)
}

func testSystemdContainerDetectedLibSystemd(t *testing.T) {
	t.Helper()

	p := &plugin{}
	pod := &api.PodSandbox{
		Name:        "test-pod-systemd",
		Annotations: map[string]string{},
	}
	container := &api.Container{
		Name:        "test-container-systemd",
		Annotations: map[string]string{},
		Args:        []string{"/lib/systemd/systemd"},
		Linux:       &api.LinuxContainer{},
		Mounts: []*api.Mount{
			{
				Destination: "/sys/fs/cgroup",
				Type:        "cgroup",
				Source:      "cgroup",
				Options:     []string{"nosuid", "noexec", "nodev", "relatime", "rw"},
			},
		},
		Env: []string{"PATH=/usr/bin"},
		Id:  "test-container-id-12345",
	}

	adjust, updates, err := p.CreateContainer(context.Background(), pod, container)

	assert.NoError(t, err)
	assert.NotNil(t, adjust)
	assert.Nil(t, updates)

	assert.NotNil(t, adjust.Mounts)
	assert.NotNil(t, adjust.Env)
}

func testSystemdContainerDetectedUsrLibSystemd(t *testing.T) {
	t.Helper()

	p := &plugin{}
	pod := &api.PodSandbox{
		Name:        "test-pod-systemd",
		Annotations: map[string]string{},
	}
	container := &api.Container{
		Name:        "test-container-systemd",
		Annotations: map[string]string{},
		Args:        []string{"/usr/lib/systemd/systemd"},
		Linux:       &api.LinuxContainer{},
		Mounts: []*api.Mount{
			{
				Destination: "/sys/fs/cgroup",
				Type:        "cgroup",
				Source:      "cgroup",
				Options:     []string{"nosuid", "noexec", "nodev", "relatime", "rw"},
			},
		},
		Env: []string{"PATH=/usr/bin"},
		Id:  "test-container-id-12345",
	}

	adjust, updates, err := p.CreateContainer(context.Background(), pod, container)

	assert.NoError(t, err)
	assert.NotNil(t, adjust)
	assert.Nil(t, updates)

	assert.NotNil(t, adjust.Mounts)
	assert.NotNil(t, adjust.Env)
}

func TestIsSystemdContainer(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "empty args",
			args:     []string{},
			expected: false,
		},
		{
			name:     "bash command",
			args:     []string{"/bin/bash"},
			expected: false,
		},
		{
			name:     "sh command",
			args:     []string{"/bin/sh"},
			expected: false,
		},
		{
			name:     "sleep command",
			args:     []string{"sleep", "infinity"},
			expected: false,
		},
		{
			name:     "/sbin/init",
			args:     []string{"/sbin/init"},
			expected: true,
		},
		{
			name:     "/lib/systemd/systemd",
			args:     []string{"/lib/systemd/systemd"},
			expected: true,
		},
		{
			name:     "/usr/lib/systemd/systemd",
			args:     []string{"/usr/lib/systemd/systemd"},
			expected: true,
		},
		{
			name:     "/sbin/init with args",
			args:     []string{"/sbin/init", "--log-target=journal"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &api.Container{
				Args: tt.args,
			}
			result := isSystemdContainer(container)
			assert.Equal(t, tt.expected, result)
		})
	}
}
