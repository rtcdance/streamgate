package core

import (
	"fmt"
	"sync"

	"github.com/rtcdance/streamgate/pkg/core/config"

	"go.uber.org/zap"
)

type PluginFactory func(cfg *config.Config, logger *zap.Logger) Plugin

var (
	factoryMu       sync.RWMutex
	pluginFactories = make(map[string]PluginFactory)
)

func RegisterPluginFactory(name string, factory PluginFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()

	if _, exists := pluginFactories[name]; exists {
		panic(fmt.Sprintf("plugin factory %q already registered", name))
	}
	pluginFactories[name] = factory
}

func MustRegisterPluginFactory(name string, factory PluginFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	pluginFactories[name] = factory
}

func GetPluginFactory(name string) PluginFactory {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	return pluginFactories[name]
}

func RegisteredPluginNames() []string {
	factoryMu.RLock()
	defer factoryMu.RUnlock()

	names := make([]string, 0, len(pluginFactories))
	for name := range pluginFactories {
		names = append(names, name)
	}
	return names
}

func (m *Microkernel) LoadRegisteredPlugins() error {
	factoryMu.RLock()
	factories := make(map[string]PluginFactory, len(pluginFactories))
	for k, v := range pluginFactories {
		factories[k] = v
	}
	factoryMu.RUnlock()

	for name, factory := range factories {
		plugin := factory(m.config, m.logger)
		if err := m.RegisterPlugin(plugin); err != nil {
			return fmt.Errorf("failed to register plugin %q: %w", name, err)
		}
	}

	return nil
}
