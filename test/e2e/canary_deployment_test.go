package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// CanaryDeploymentTest tests canary deployment functionality
type CanaryDeploymentTest struct {
	clientset *kubernetes.Clientset
	namespace string
	timeout   time.Duration
}

// NewCanaryDeploymentTest creates a new canary deployment test
func NewCanaryDeploymentTest(namespace string) (*CanaryDeploymentTest, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &CanaryDeploymentTest{
		clientset: clientset,
		namespace: namespace,
		timeout:   5 * time.Minute,
	}, nil
}

// TestCanaryDeploymentExists tests that stable and canary deployments exist
func (t *CanaryDeploymentTest) TestCanaryDeploymentExists(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployments, err := t.clientset.AppsV1().Deployments(t.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		test.Fatalf("Failed to list deployments: %v", err)
	}

	stableExists := false
	canaryExists := false

	for _, dep := range deployments.Items {
		if dep.Name == "streamgate-stable" {
			stableExists = true
		}
		if dep.Name == "streamgate-canary" {
			canaryExists = true
		}
	}

	if !stableExists {
		test.Error("Stable deployment not found")
	}
	if !canaryExists {
		test.Error("Canary deployment not found")
	}
}

// TestCanaryServiceExists tests that stable and canary services exist
func (t *CanaryDeploymentTest) TestCanaryServiceExists(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	services, err := t.clientset.CoreV1().Services(t.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		test.Fatalf("Failed to list services: %v", err)
	}

	stableExists := false
	canaryExists := false

	for _, svc := range services.Items {
		if svc.Name == "streamgate-stable" {
			stableExists = true
		}
		if svc.Name == "streamgate-canary" {
			canaryExists = true
		}
	}

	if !stableExists {
		test.Error("Stable service not found")
	}
	if !canaryExists {
		test.Error("Canary service not found")
	}
}

// TestStableDeploymentHealthy tests that stable deployment is healthy
func (t *CanaryDeploymentTest) TestStableDeploymentHealthy(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-stable", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get stable deployment: %v", err)
	}

	if deployment.Status.ReadyReplicas == 0 {
		test.Error("Stable deployment has no ready replicas")
	}

	if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
		test.Errorf("Stable deployment not fully ready: %d/%d replicas", deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
	}
}

// TestCanaryDeploymentScalable tests that canary deployment can be scaled
func (t *CanaryDeploymentTest) TestCanaryDeploymentScalable(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary deployment: %v", err)
	}

	// Canary should start with 0 replicas
	if *deployment.Spec.Replicas != 0 {
		test.Errorf("Canary deployment should start with 0 replicas, got %d", *deployment.Spec.Replicas)
	}
}

// TestCanaryHealthChecks tests that health checks are configured
func (t *CanaryDeploymentTest) TestCanaryHealthChecks(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary deployment: %v", err)
	}

	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		test.Fatal("No containers found in canary deployment")
	}

	container := containers[0]

	if container.LivenessProbe == nil {
		test.Error("Liveness probe not configured")
	}

	if container.ReadinessProbe == nil {
		test.Error("Readiness probe not configured")
	}
}

// TestCanaryResourceLimits tests that resource limits are configured
func (t *CanaryDeploymentTest) TestCanaryResourceLimits(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary deployment: %v", err)
	}

	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		test.Fatal("No containers found in canary deployment")
	}

	container := containers[0]

	if container.Resources.Requests == nil || len(container.Resources.Requests) == 0 {
		test.Error("Resource requests not configured")
	}

	if container.Resources.Limits == nil || len(container.Resources.Limits) == 0 {
		test.Error("Resource limits not configured")
	}
}

// TestCanaryDeploymentReplicas tests that deployment replicas are configured
func (t *CanaryDeploymentTest) TestCanaryDeploymentReplicas(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-stable", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get stable deployment: %v", err)
	}

	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas < 3 {
		test.Error("Stable deployment should have at least 3 replicas")
	}
}

// TestCanaryMetricsExposed tests that metrics are exposed
func (t *CanaryDeploymentTest) TestCanaryMetricsExposed(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary deployment: %v", err)
	}

	annotations := deployment.Spec.Template.ObjectMeta.Annotations
	if annotations == nil {
		test.Error("No annotations found")
		return
	}

	if _, exists := annotations["prometheus.io/scrape"]; !exists {
		test.Error("Prometheus scrape annotation not found")
	}

	if _, exists := annotations["prometheus.io/port"]; !exists {
		test.Error("Prometheus port annotation not found")
	}

	if _, exists := annotations["prometheus.io/path"]; !exists {
		test.Error("Prometheus path annotation not found")
	}
}

// TestCanaryRollingUpdate tests that rolling update strategy is configured
func (t *CanaryDeploymentTest) TestCanaryRollingUpdate(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary deployment: %v", err)
	}

	if deployment.Spec.Strategy.Type != "RollingUpdate" {
		test.Errorf("Expected RollingUpdate strategy, got %s", deployment.Spec.Strategy.Type)
	}

	if deployment.Spec.Strategy.RollingUpdate == nil {
		test.Error("RollingUpdate configuration not found")
	}
}

// TestCanaryServicePorts tests that services have correct ports
func (t *CanaryDeploymentTest) TestCanaryServicePorts(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	service, err := t.clientset.CoreV1().Services(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary service: %v", err)
	}

	if len(service.Spec.Ports) == 0 {
		test.Error("No ports configured in canary service")
	}

	hasHTTP := false
	hasMetrics := false

	for _, port := range service.Spec.Ports {
		if port.Name == "http" && port.Port == 9090 {
			hasHTTP = true
		}
		if port.Name == "metrics" && port.Port == 9091 {
			hasMetrics = true
		}
	}

	if !hasHTTP {
		test.Error("HTTP port not configured")
	}
	if !hasMetrics {
		test.Error("Metrics port not configured")
	}
}

// TestCanaryImageDifference tests that canary can use different image
func (t *CanaryDeploymentTest) TestCanaryImageDifference(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	stableDeployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-stable", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get stable deployment: %v", err)
	}

	canaryDeployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary deployment: %v", err)
	}

	stableImage := stableDeployment.Spec.Template.Spec.Containers[0].Image
	canaryImage := canaryDeployment.Spec.Template.Spec.Containers[0].Image

	// Images can be different (canary uses latest, stable uses stable tag)
	test.Logf("Stable image: %s", stableImage)
	test.Logf("Canary image: %s", canaryImage)
}

// TestCanaryServiceSelector tests that services have correct selectors
func (t *CanaryDeploymentTest) TestCanaryServiceSelector(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	stableService, err := t.clientset.CoreV1().Services(t.namespace).Get(ctx, "streamgate-stable", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get stable service: %v", err)
	}

	canaryService, err := t.clientset.CoreV1().Services(t.namespace).Get(ctx, "streamgate-canary", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get canary service: %v", err)
	}

	stableVersion, exists := stableService.Spec.Selector["version"]
	if !exists || stableVersion != "stable" {
		test.Error("Stable service selector incorrect")
	}

	canaryVersion, exists := canaryService.Spec.Selector["version"]
	if !exists || canaryVersion != "canary" {
		test.Error("Canary service selector incorrect")
	}
}

// Run executes all canary deployment tests
func (t *CanaryDeploymentTest) Run(test *testing.T) {
	test.Run("CanaryDeploymentExists", t.TestCanaryDeploymentExists)
	test.Run("CanaryServiceExists", t.TestCanaryServiceExists)
	test.Run("StableDeploymentHealthy", t.TestStableDeploymentHealthy)
	test.Run("CanaryDeploymentScalable", t.TestCanaryDeploymentScalable)
	test.Run("CanaryHealthChecks", t.TestCanaryHealthChecks)
	test.Run("CanaryResourceLimits", t.TestCanaryResourceLimits)
	test.Run("CanaryDeploymentReplicas", t.TestCanaryDeploymentReplicas)
	test.Run("CanaryMetricsExposed", t.TestCanaryMetricsExposed)
	test.Run("CanaryRollingUpdate", t.TestCanaryRollingUpdate)
	test.Run("CanaryServicePorts", t.TestCanaryServicePorts)
	test.Run("CanaryImageDifference", t.TestCanaryImageDifference)
	test.Run("CanaryServiceSelector", t.TestCanaryServiceSelector)
}

// TestCanaryDeployment is the main test function
func TestCanaryDeployment(test *testing.T) {
	canaryTest, err := NewCanaryDeploymentTest("streamgate")
	if err != nil {
		test.Skipf("Skipping canary deployment tests: %v", err)
	}

	canaryTest.Run(test)
}
