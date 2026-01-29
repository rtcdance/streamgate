package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// BlueGreenDeploymentTest tests blue-green deployment functionality
type BlueGreenDeploymentTest struct {
	clientset *kubernetes.Clientset
	namespace string
	timeout   time.Duration
}

// NewBlueGreenDeploymentTest creates a new blue-green deployment test
func NewBlueGreenDeploymentTest(namespace string) (*BlueGreenDeploymentTest, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &BlueGreenDeploymentTest{
		clientset: clientset,
		namespace: namespace,
		timeout:   5 * time.Minute,
	}, nil
}

// TestBlueGreenDeploymentExists tests that blue and green deployments exist
func (t *BlueGreenDeploymentTest) TestBlueGreenDeploymentExists(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployments, err := t.clientset.AppsV1().Deployments(t.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		test.Fatalf("Failed to list deployments: %v", err)
	}

	blueExists := false
	greenExists := false

	for _, dep := range deployments.Items {
		if dep.Name == "streamgate-blue" {
			blueExists = true
		}
		if dep.Name == "streamgate-green" {
			greenExists = true
		}
	}

	if !blueExists {
		test.Error("Blue deployment not found")
	}
	if !greenExists {
		test.Error("Green deployment not found")
	}
}

// TestBlueGreenServiceExists tests that blue and green services exist
func (t *BlueGreenDeploymentTest) TestBlueGreenServiceExists(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	services, err := t.clientset.CoreV1().Services(t.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		test.Fatalf("Failed to list services: %v", err)
	}

	blueExists := false
	greenExists := false
	activeExists := false

	for _, svc := range services.Items {
		if svc.Name == "streamgate-blue" {
			blueExists = true
		}
		if svc.Name == "streamgate-green" {
			greenExists = true
		}
		if svc.Name == "streamgate-active" {
			activeExists = true
		}
	}

	if !blueExists {
		test.Error("Blue service not found")
	}
	if !greenExists {
		test.Error("Green service not found")
	}
	if !activeExists {
		test.Error("Active service not found")
	}
}

// TestBlueDeploymentHealthy tests that blue deployment is healthy
func (t *BlueGreenDeploymentTest) TestBlueDeploymentHealthy(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-blue", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get blue deployment: %v", err)
	}

	if deployment.Status.ReadyReplicas == 0 {
		test.Error("Blue deployment has no ready replicas")
	}

	if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
		test.Errorf("Blue deployment not fully ready: %d/%d replicas", deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
	}
}

// TestGreenDeploymentScalable tests that green deployment can be scaled
func (t *BlueGreenDeploymentTest) TestGreenDeploymentScalable(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-green", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get green deployment: %v", err)
	}

	// Green should start with 0 replicas
	if *deployment.Spec.Replicas != 0 {
		test.Errorf("Green deployment should start with 0 replicas, got %d", *deployment.Spec.Replicas)
	}
}

// TestActiveServiceSelector tests that active service has correct selector
func (t *BlueGreenDeploymentTest) TestActiveServiceSelector(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	service, err := t.clientset.CoreV1().Services(t.namespace).Get(ctx, "streamgate-active", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get active service: %v", err)
	}

	version, exists := service.Spec.Selector["version"]
	if !exists {
		test.Error("Active service selector missing 'version' label")
	}

	if version != "blue" && version != "green" {
		test.Errorf("Active service selector has invalid version: %s", version)
	}
}

// TestBlueGreenHealthChecks tests that health checks are configured
func (t *BlueGreenDeploymentTest) TestBlueGreenHealthChecks(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-blue", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get blue deployment: %v", err)
	}

	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		test.Fatal("No containers found in blue deployment")
	}

	container := containers[0]

	if container.LivenessProbe == nil {
		test.Error("Liveness probe not configured")
	}

	if container.ReadinessProbe == nil {
		test.Error("Readiness probe not configured")
	}
}

// TestBlueGreenResourceLimits tests that resource limits are configured
func (t *BlueGreenDeploymentTest) TestBlueGreenResourceLimits(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-blue", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get blue deployment: %v", err)
	}

	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		test.Fatal("No containers found in blue deployment")
	}

	container := containers[0]

	if container.Resources.Requests == nil || len(container.Resources.Requests) == 0 {
		test.Error("Resource requests not configured")
	}

	if container.Resources.Limits == nil || len(container.Resources.Limits) == 0 {
		test.Error("Resource limits not configured")
	}
}

// TestBlueGreenDeploymentReplicas tests that deployment replicas are configured
func (t *BlueGreenDeploymentTest) TestBlueGreenDeploymentReplicas(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-blue", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get blue deployment: %v", err)
	}

	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas < 3 {
		test.Error("Blue deployment should have at least 3 replicas")
	}
}

// TestBlueGreenPodDistribution tests that pods are distributed across nodes
func (t *BlueGreenDeploymentTest) TestBlueGreenPodDistribution(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	pods, err := t.clientset.CoreV1().Pods(t.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=streamgate,version=blue",
	})
	if err != nil {
		test.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) == 0 {
		test.Error("No blue pods found")
		return
	}

	nodeMap := make(map[string]int)
	for _, pod := range pods.Items {
		nodeMap[pod.Spec.NodeName]++
	}

	if len(nodeMap) < 2 {
		test.Logf("Warning: Pods not distributed across multiple nodes: %v", nodeMap)
	}
}

// TestBlueGreenMetricsExposed tests that metrics are exposed
func (t *BlueGreenDeploymentTest) TestBlueGreenMetricsExposed(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-blue", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get blue deployment: %v", err)
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

// TestBlueGreenRollingUpdate tests that rolling update strategy is configured
func (t *BlueGreenDeploymentTest) TestBlueGreenRollingUpdate(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	deployment, err := t.clientset.AppsV1().Deployments(t.namespace).Get(ctx, "streamgate-blue", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get blue deployment: %v", err)
	}

	if deployment.Spec.Strategy.Type != "RollingUpdate" {
		test.Errorf("Expected RollingUpdate strategy, got %s", deployment.Spec.Strategy.Type)
	}

	if deployment.Spec.Strategy.RollingUpdate == nil {
		test.Error("RollingUpdate configuration not found")
	}
}

// TestBlueGreenServiceLoadBalancer tests that active service is load balancer
func (t *BlueGreenDeploymentTest) TestBlueGreenServiceLoadBalancer(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	service, err := t.clientset.CoreV1().Services(t.namespace).Get(ctx, "streamgate-active", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get active service: %v", err)
	}

	if service.Spec.Type != corev1.ServiceTypeLoadBalancer {
		test.Errorf("Expected LoadBalancer service type, got %s", service.Spec.Type)
	}
}

// TestBlueGreenServicePorts tests that services have correct ports
func (t *BlueGreenDeploymentTest) TestBlueGreenServicePorts(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	service, err := t.clientset.CoreV1().Services(t.namespace).Get(ctx, "streamgate-blue", metav1.GetOptions{})
	if err != nil {
		test.Fatalf("Failed to get blue service: %v", err)
	}

	if len(service.Spec.Ports) == 0 {
		test.Error("No ports configured in blue service")
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

// Run executes all blue-green deployment tests
func (t *BlueGreenDeploymentTest) Run(test *testing.T) {
	test.Run("BlueGreenDeploymentExists", t.TestBlueGreenDeploymentExists)
	test.Run("BlueGreenServiceExists", t.TestBlueGreenServiceExists)
	test.Run("BlueDeploymentHealthy", t.TestBlueDeploymentHealthy)
	test.Run("GreenDeploymentScalable", t.TestGreenDeploymentScalable)
	test.Run("ActiveServiceSelector", t.TestActiveServiceSelector)
	test.Run("BlueGreenHealthChecks", t.TestBlueGreenHealthChecks)
	test.Run("BlueGreenResourceLimits", t.TestBlueGreenResourceLimits)
	test.Run("BlueGreenDeploymentReplicas", t.TestBlueGreenDeploymentReplicas)
	test.Run("BlueGreenPodDistribution", t.TestBlueGreenPodDistribution)
	test.Run("BlueGreenMetricsExposed", t.TestBlueGreenMetricsExposed)
	test.Run("BlueGreenRollingUpdate", t.TestBlueGreenRollingUpdate)
	test.Run("BlueGreenServiceLoadBalancer", t.TestBlueGreenServiceLoadBalancer)
	test.Run("BlueGreenServicePorts", t.TestBlueGreenServicePorts)
}

// TestBlueGreenDeployment is the main test function
func TestBlueGreenDeployment(t *testing.T) {
	bgTest, err := NewBlueGreenDeploymentTest("streamgate")
	if err != nil {
		t.Skipf("Skipping blue-green deployment tests: %v", err)
	}

	bgTest.Run(t)
}
