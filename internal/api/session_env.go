package api

import (
	"os"

	"github.com/gastownhall/gascity/internal/providerenv"
)

func providerSessionEnv(env map[string]string) map[string]string {
	out := providerenv.ManagedSessionBaseline()
	for key, value := range env {
		out[key] = os.ExpandEnv(value)
	}
	return out
}
