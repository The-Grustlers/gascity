package api

import "github.com/gastownhall/gascity/internal/providerenv"

func providerSessionEnv(env map[string]string) map[string]string {
	return providerenv.MergeManagedSessionEnv(env)
}
