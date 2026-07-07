package runtime

import provider "noah/oracle/sidecar/providers/base"

func GetProvidersForTest(r *Runtime) map[string]*provider.Provider {
	return r.getProviders()
}
