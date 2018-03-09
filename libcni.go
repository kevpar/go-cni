package libcni

import (
	"sort"
	"strings"

	cnilibrary "github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/pkg/errors"
)

type CNI interface {
	// Status returns whether the cni plugin is ready.
	Status() error
	// Setup setups the networking for the namespace.
	Setup(id string, path string, opts ...NamespaceOpts) (*CNIResult, error)
	// Remove tears down the network of the namespace.
	Remove(id string, path string, opts ...NamespaceOpts) error
}

type libcni struct {
	config

	cniConfig    cnilibrary.CNI
	networkCount int // minimum network plugin configurations needed to initialize cni
	networks     []*Network
}

func defaultCNIConfig() *libcni {
	return &libcni{
		config: config{
			pluginDirs:    []string{DefaultCNIDir},
			pluginConfDir: DefaultNetDir,
			prefix:        DefaultPrefix,
		},
	}
}

func New(config ...ConfigOptions) CNI {
	cni := defaultCNIConfig()
	for _, c := range config {
		c(cni)
	}
	cni.populateNetworkConfig()
	return cni
}

// Status checks the status of cni initialization
func (c *libcni) Status() error {
	// TODO this logic changes when CNI Supports
	// Dynamic network updates
	if len(c.networks) < c.networkCount {
		err := c.populateNetworkConfig()
		if err != nil {
			return err
		}
	}

	if len(c.networks) < c.networkCount {
		return ErrCNINotInitialized
	}
	return nil
}

func (c *libcni) populateNetworkConfig() error {
	files, err := cnilibrary.ConfFiles(c.pluginConfDir, []string{".conf", ".conflist", ".json"})
	switch {
	case err != nil:
		return errors.Wrapf(ErrRead, "failed to read config file: %v", err)
	case len(files) == 0:
		return errors.Wrapf(ErrCNINotInitialized, "no network config found in %s", c.pluginConfDir)
	}

	// files contains the network config files associated with cni network.
	// Use lexicographical way as a defined order for network config files.
	sort.Strings(files)
	// Since the CNI spec does not specify a way to detect default networks,
	// the convention chosen is - the first network configuration in the sorted
	// list of network conf files as the default network and choose the default
	// interface provided during init as the network interface for this default
	// network. For every other network use a generated interface id.
	i := 0
	for _, confFile := range files {
		var confList *cnilibrary.NetworkConfigList
		if strings.HasSuffix(confFile, ".conflist") {
			confList, err = cnilibrary.ConfListFromFile(confFile)
			if err != nil {
				return errors.Wrapf(ErrInvalidConfig, "failed to load CNI config list file %s: %v", confFile, err)
			}
		} else {
			conf, err := cnilibrary.ConfFromFile(confFile)
			if err != nil {
				return errors.Wrapf(ErrInvalidConfig, "failed to load CNI config file %s: %v", confFile, err)
			}
			// Ensure the config has a "type" so we know what plugin to run.
			// Also catches the case where somebody put a conflist into a conf file.
			if conf.Network.Type == "" {
				return errors.Wrapf(ErrInvalidConfig, "network type not found in %s", confFile)
			}

			confList, err = cnilibrary.ConfListFromConf(conf)
			if err != nil {
				return errors.Wrapf(ErrInvalidConfig, "failed to convert CNI config file %s to list: %v", confFile, err)
			}
		}
		if len(confList.Plugins) == 0 {
			return errors.Wrapf(ErrInvalidConfig, "CNI config list %s has no networks, skipping", confFile)

		}
		c.networks = append(c.networks, &Network{
			cni:    c.cniConfig,
			config: confList,
			ifName: getIfName(c.prefix, i),
		})
		i++
	}
	if len(c.networks) == 0 {
		return errors.Wrapf(ErrCNINotInitialized, "no valid networks found in %s", c.pluginDirs)
	}
	return nil
}

// Setup setups the network in the namespace
func (c *libcni) Setup(id string, path string, opts ...NamespaceOpts) (*CNIResult, error) {
	ns, err := newNamespace(id, path, opts...)
	if err != nil {
		return nil, err
	}
	var results []*current.Result
	for _, network := range c.networks {
		r, err := network.Attach(ns)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return c.GetCNIResultFromResults(results)
}

// Remove removes the network config from the namespace
func (c *libcni) Remove(id string, path string, opts ...NamespaceOpts) error {
	ns, err := newNamespace(id, path, opts...)
	if err != nil {
		return err
	}
	for _, network := range c.networks {
		if err := network.Remove(ns); err != nil {
			return err
		}
	}
	return nil
}
