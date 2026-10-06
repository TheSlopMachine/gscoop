//go:build windows

package install

import (
	"golang.org/x/sys/windows/registry"
)

// registryStore reads HKCU/HKLM Environment, mirroring Get-EnvVar and
// Set-EnvVar (lib/system.ps1:30-74) with WM_SETTINGCHANGE broadcast.
type registryStore struct{}

func envKey(global bool) (registry.Key, string) {
	if global {
		return registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	}
	return registry.CURRENT_USER, `Environment`
}

func (registryStore) Get(name string, global bool) string {
	root, path := envKey(global)
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	val, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return val
}

func (registryStore) Set(name, value string, global bool) error {
	root, path := envKey(global)
	k, err := registry.OpenKey(root, path, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if value == "" {
		_ = k.DeleteValue(name)
	} else {
		_ = k.SetStringValue(name, value)
	}
	broadcastSettingChange()
	return nil
}
