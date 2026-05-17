variable "GO_VERSION" {
  default = "1.24"
}

variable "REGISTRY" {
  default = "streamgate"
}

# ─── Groups ───────────────────────────────────────────────────

group "default" {
  targets = ["monolith"]
}

group "all" {
  targets = [
    "monolith",
    "api-gateway",
    "auth",
    "streaming",
    "transcoder",
    "upload",
    "metadata",
    "cache",
    "worker",
    "monitor",
  ]
}

group "core" {
  targets = ["api-gateway", "auth", "streaming"]
}

# ─── Targets ──────────────────────────────────────────────────

target "monolith" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/monolith/streamgate"
    EXTRA_PKGS = "ffmpeg"
  }
  tags = ["${REGISTRY}/monolith:latest"]
}

target "api-gateway" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/api-gateway"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/api-gateway:latest"]
}

target "auth" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/auth"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/auth:latest"]
}

target "streaming" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/streaming"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/streaming:latest"]
}

target "transcoder" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/transcoder"
    EXTRA_PKGS = "ffmpeg"
  }
  tags = ["${REGISTRY}/transcoder:latest"]
}

target "upload" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/upload"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/upload:latest"]
}

target "metadata" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/metadata"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/metadata:latest"]
}

target "cache" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/cache"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/cache:latest"]
}

target "worker" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/worker"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/worker:latest"]
}

target "monitor" {
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
    PKG        = "./cmd/microservices/monitor"
    EXTRA_PKGS = ""
  }
  tags = ["${REGISTRY}/monitor:latest"]
}
