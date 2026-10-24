// Copyright 2020 Nokia
// Licensed under the BSD 3-Clause License.
// SPDX-License-Identifier: BSD-3-Clause

package unmanaged_switch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/containernetworking/plugins/pkg/ns"
	clablinks "github.com/srl-labs/containerlab/links"
	clabnodes "github.com/srl-labs/containerlab/nodes"
	clabtypes "github.com/srl-labs/containerlab/types"
	"github.com/vishvananda/netlink"
)

const (
	generateable     = true
	generateIfFormat = "eth%d"

	defaultImage = "alpine:3"
	defaultCmd   = "sleep infinity"

	// bridgeName is the name of the bridge inside the switch container.
	bridgeName = "br0"
)

var kindNames = []string{"switch"}

// Register registers the node in the NodeRegistry.
func Register(r *clabnodes.NodeRegistry) {
	generateNodeAttributes := clabnodes.NewGenerateNodeAttributes(generateable, generateIfFormat)
	nrea := clabnodes.NewNodeRegistryEntryAttributes(nil, generateNodeAttributes, nil)

	r.Register(kindNames, func() clabnodes.Node {
		return new(unmanagedSwitch)
	}, nrea)
}

// unmanagedSwitch is a container holding a network namespace with a bridge
// that all of the node's endpoints are attached to.
type unmanagedSwitch struct {
	clabnodes.DefaultNode
}

func (*unmanagedSwitch) LinkApplyMode(context.Context) clabnodes.LinkApplyMode {
	return clabnodes.LinkApplyModeLive
}

func (n *unmanagedSwitch) Init(cfg *clabtypes.NodeConfig, opts ...clabnodes.NodeOption) error {
	// Init DefaultNode
	n.DefaultNode = *clabnodes.NewDefaultNode(n)
	n.Cfg = cfg

	// sleep ignores SIGTERM as PID 1
	n.StopSignal = clabtypes.SIGKILL

	if n.Cfg.Image == "" {
		n.Cfg.Image = defaultImage
	}
	if n.Cfg.Cmd == "" {
		n.Cfg.Cmd = defaultCmd
	}
	if n.Cfg.NetworkMode == "" {
		n.Cfg.NetworkMode = "none"
	}

	for _, o := range opts {
		o(n)
	}

	nm := strings.ToLower(n.Cfg.NetworkMode)
	if nm == "host" || strings.HasPrefix(nm, "container:") {
		return fmt.Errorf("network-mode %q is not supported by %s node %q",
			n.Cfg.NetworkMode, n.Cfg.Kind, n.Cfg.ShortName)
	}

	// keep the switch from sending IPv6 ND and MLD messages
	if n.Cfg.Sysctls == nil {
		n.Cfg.Sysctls = map[string]string{}
	}
	n.Cfg.Sysctls["net.ipv6.conf.all.disable_ipv6"] = "1"
	n.Cfg.Sysctls["net.ipv6.conf.default.disable_ipv6"] = "1"

	return nil
}

// Start recreates the bridge, which is lost with the container's netns on stop, and attaches the
// restored endpoints to it. DefaultNode restores them without calling our AddLinkToContainer.
func (n *unmanagedSwitch) Start(ctx context.Context) error {
	if err := n.DefaultNode.Start(ctx); err != nil {
		return err
	}

	return n.ExecFunction(ctx, func(_ ns.NetNS) error {
		br, err := ensureBridge()
		if err != nil {
			return err
		}

		for _, ep := range n.GetEndpoints() {
			link, err := netlink.LinkByName(ep.GetIfaceName())
			if err != nil {
				return err
			}

			if err := netlink.LinkSetMaster(link, br); err != nil {
				return err
			}
		}

		return nil
	})
}

func (n *unmanagedSwitch) AddLinkToContainer(
	ctx context.Context,
	link netlink.Link,
	f func(ns.NetNS) error,
) error {
	return n.DefaultNode.AddLinkToContainer(ctx, link, func(nn ns.NetNS) error {
		if err := f(nn); err != nil {
			return err
		}

		br, err := ensureBridge()
		if err != nil {
			return err
		}

		return netlink.LinkSetMaster(link, br)
	})
}

// ensureBridge creates the bridge in the current netns if it does not exist yet.
func ensureBridge() (netlink.Link, error) {
	snooping := false
	err := netlink.LinkAdd(&netlink.Bridge{
		LinkAttrs:         netlink.LinkAttrs{Name: bridgeName},
		MulticastSnooping: &snooping,
	})
	// links may be attached concurrently
	if err != nil && !errors.Is(err, syscall.EEXIST) {
		return nil, err
	}

	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, err
	}

	return br, netlink.LinkSetUp(br)
}

// CheckInterfaceName checks that eth0 is not used, since it is the management interface name
// and is skipped by the lifecycle commands.
func (n *unmanagedSwitch) CheckInterfaceName() error {
	for _, e := range n.Endpoints {
		if e.GetIfaceName() == "eth0" {
			return fmt.Errorf("eth0 interface name is not allowed for %s node", n.Cfg.ShortName)
		}

		if !clablinks.IsValidInterfaceName(e.GetIfaceName()) {
			return fmt.Errorf("interface name %q of %s node is not a valid linux interface name",
				e.GetIfaceName(), n.Cfg.ShortName)
		}
	}

	return n.CheckInterfaceOverlap()
}
