// Shared endpoint resolution for the acceptance suite.
//
// Defaults match `make fullchain-deploy` on the host (host-published ports).
// To run this suite inside a container on the compose network, point the bases at
// the service names instead — see scripts/docker-acceptance.sh in the
// streamgate-docker-acceptance skill:
//
//   H5_BASE=http://h5-demo:8080
//   API_BASE=http://api-gateway:8080
//   MONOLITH_BASE=http://monolith:8080
//   ANVIL_BASE=http://anvil:8545
//
// Every consumer must import from here rather than hardcoding a URL, so the same
// specs run both on the host and inside Docker without edits.

export const H5_BASE = process.env.H5_BASE || 'http://localhost:18000';
export const API_BASE = process.env.API_BASE || 'http://localhost:28080';
export const MONOLITH_BASE = process.env.MONOLITH_BASE || 'http://localhost:18080';
export const ANVIL_BASE = process.env.ANVIL_BASE || 'http://localhost:18545';

// Playwright browser channel. The host flow uses the installed Google Chrome
// (`channel: 'chrome'`) to avoid downloading Chromium. Inside the official
// Playwright image only the bundled Chromium exists, so set PW_CHANNEL='' there.
export const PW_CHANNEL = process.env.PW_CHANNEL === undefined ? 'chrome' : process.env.PW_CHANNEL;

// Only these hosts are expected to answer with /health during a probe; used to
// filter the cold-start 503 that is expected while a service is still booting.
export const HEALTH_HOSTS = [API_BASE, MONOLITH_BASE];
