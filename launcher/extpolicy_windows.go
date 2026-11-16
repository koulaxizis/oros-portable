package main

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// policyKey is where Chromium (and so ungoogled-chromium) reads a user's
// policies. Edge, Chrome and Brave read their own keys.
const policyKey = `Software\Policies\Chromium`

// setExtPolicy writes ExtensionSettings for this user. A value someone
// else set (their own Chromium policy) stays as it is. A portable copy
// takes its value back when orOS closes; the desktop apps keep it, as
// they keep the native host's key.
func setExtPolicy(lay layout, value string) (func(), error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, policyKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	if have, _, err := k.GetStringValue("ExtensionSettings"); err == nil && have != value && !isOurPolicy(have) {
		return nil, errors.New("ExtensionSettings is already set by someone else; left as it is")
	}
	if err := k.SetStringValue("ExtensionSettings", value); err != nil {
		return nil, err
	}
	if lay.installed {
		return nil, nil
	}
	return func() {
		k, err := registry.OpenKey(registry.CURRENT_USER, policyKey, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		if v, _, _ := k.GetStringValue("ExtensionSettings"); v == value {
			_ = k.DeleteValue("ExtensionSettings")
		}
	}, nil
}
