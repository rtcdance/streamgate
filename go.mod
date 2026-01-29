module streamgate

go 1.21

require (
	github.com/gin-gonic/gin v1.9.1
	github.com/google/uuid v1.5.0
	github.com/stretchr/testify v1.8.4
	go.uber.org/zap v1.26.0
	gopkg.in/yaml.v2 v2.4.0
	
	// Database
	github.com/lib/pq v1.10.9
	github.com/go-redis/redis/v8 v8.11.5
	
	// Object Storage
	github.com/aws/aws-sdk-go v1.44.0
	github.com/minio/minio-go/v7 v7.0.63
	
	// Authentication
	github.com/golang-jwt/jwt/v4 v4.5.0
	golang.org/x/crypto v0.14.0
	
	// Additional dependencies for microservices
	google.golang.org/grpc v1.56.0
	google.golang.org/protobuf v1.30.0
	github.com/nats-io/nats.go v1.28.0
	github.com/hashicorp/consul/api v1.25.1
	github.com/spf13/viper v1.16.0
)
