module streamgate

go 1.24.0

require (
	// Object Storage
	github.com/aws/aws-sdk-go v1.44.0
	github.com/gin-gonic/gin v1.9.1
	github.com/go-redis/redis/v8 v8.11.5

	// Authentication
	github.com/golang-jwt/jwt/v4 v4.5.0
	github.com/google/uuid v1.5.0
	github.com/hashicorp/consul/api v1.25.1

	// Database
	github.com/lib/pq v1.10.9
	github.com/minio/minio-go/v7 v7.0.63
	github.com/nats-io/nats.go v1.28.0
	github.com/spf13/viper v1.16.0
	github.com/stretchr/testify v1.11.1
	go.uber.org/zap v1.26.0
	golang.org/x/crypto v0.14.0

	// Additional dependencies for microservices
	google.golang.org/grpc v1.56.0
	google.golang.org/protobuf v1.30.0
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	gopkg.in/ini.v1 v1.67.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
