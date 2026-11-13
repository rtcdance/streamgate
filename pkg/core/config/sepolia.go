package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

func applySepoliaInputs(cfg *Config) error {
	primary := firstNonEmpty(os.Getenv("SEPOLIA_RPC_URL"), os.Getenv("SEPOLIA_RPC"))
	fallback := strings.TrimSpace(os.Getenv("SEPOLIA_RPC_FALLBACK_URL"))
	if legacy := strings.TrimSpace(os.Getenv("SEPOLIA_RPC")); legacy != "" {
		if current := strings.TrimSpace(os.Getenv("SEPOLIA_RPC_URL")); current != "" && current != legacy {
			return fmt.Errorf("SEPOLIA_RPC_URL and legacy SEPOLIA_RPC disagree; both must describe the same Sepolia endpoint")
		}
	}
	if primary == "" {
		return nil
	}
	for i := range cfg.Web3.Chains {
		if cfg.Web3.Chains[i].ID != 11155111 {
			continue
		}
		endpoints := []string{primary}
		if fallback != "" && fallback != primary {
			endpoints = append(endpoints, fallback)
		}
		cfg.Web3.Chains[i].RPCs = endpoints
		cfg.Web3.Chains[i].RPC = primary
		if cfg.Web3.ChainID == 11155111 {
			cfg.Web3.EthereumRPC = primary
		}
		return nil
	}
	return fmt.Errorf("SEPOLIA_RPC_URL is set but chain 11155111 is not configured")
}

// The public testnet profile cannot inherit local signing or another chain
// through environment overrides. Refuse at load time for every entry point.
func validateSepoliaProfile(cfg *Config) error {
	if len(cfg.Transcode.Profiles) != 1 || cfg.Transcode.Profiles[0] != "360p" {
		return fmt.Errorf("w3sepolia requires one 360p auto-upload profile")
	}
	if cfg.Web3.ChainID != 11155111 || len(cfg.Web3.Chains) != 1 || cfg.Web3.Chains[0].ID != 11155111 {
		return fmt.Errorf("w3sepolia requires only the Sepolia chain 11155111")
	}
	if cfg.Features.DemoTxSigner || cfg.Web3.AnvilDeployerKey != "" || cfg.Web3.AnvilDemoContract != "" || cfg.Web3.Transaction.PrivateKeyHex != "" {
		return fmt.Errorf("w3sepolia forbids backend signing and local Anvil credentials")
	}
	if cfg.Web3.BlockTag != "safe" && cfg.Web3.BlockTag != "finalized" {
		return fmt.Errorf("w3sepolia requires a safe or finalized block tag")
	}
	chain := cfg.Web3.Chains[0]
	if len(chain.RPCs) != 2 || chain.RPC != chain.RPCs[0] || cfg.Web3.EthereumRPC != chain.RPCs[0] {
		return fmt.Errorf("w3sepolia requires two explicit consistent RPC endpoints")
	}
	hosts := make(map[string]bool)
	for _, endpoint := range chain.RPCs {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || strings.HasSuffix(parsed.Hostname(), ".invalid") {
			return fmt.Errorf("w3sepolia requires explicit HTTPS RPC endpoints")
		}
		host := strings.ToLower(parsed.Hostname())
		if hosts[host] {
			return fmt.Errorf("w3sepolia requires distinct RPC provider hosts")
		}
		hosts[host] = true
	}
	origin, err := url.Parse(cfg.Auth.SIWEURI)
	if err != nil || origin.User != nil || origin.Fragment != "" || origin.RawQuery != "" || cfg.Auth.SIWEDomain != origin.Host || origin.Path == "" {
		return fmt.Errorf("w3sepolia requires an explicit matching SIWE domain and URI")
	}
	host := origin.Hostname()
	if (host != "localhost" && host != "127.0.0.1" && host != "::1") || (origin.Scheme != "http" && origin.Scheme != "https") {
		return fmt.Errorf("w3sepolia application SIWE origin must be local loopback")
	}
	return nil
}

func validateSepoliaWorkerProfile(cfg *Config) error {
	// Media parsing stays outside both the JWT-signing and chain trust domains.
	if cfg.Auth.JWTSecret != "" || cfg.Auth.AccessSecret != "" || cfg.Auth.CapabilitySecret != "" ||
		len(cfg.Auth.PreviousAccessSecrets) != 0 || len(cfg.Auth.PreviousCapabilitySecrets) != 0 ||
		cfg.Web3.ChainID != 0 || len(cfg.Web3.Chains) != 0 || cfg.Web3.EthereumRPC != "" || cfg.Web3.EthereumWSURL != "" ||
		cfg.Web3.AnvilDeployerKey != "" || cfg.Web3.AnvilDemoContract != "" || cfg.Web3.Transaction.PrivateKeyHex != "" ||
		cfg.Web3.ContentRegistryAddress != "" || cfg.Features.DemoTxSigner {
		return fmt.Errorf("w3sepolia-worker forbids JWT keys, chain access and backend signing")
	}
	return nil
}
