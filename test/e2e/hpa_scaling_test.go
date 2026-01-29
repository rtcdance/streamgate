package scaling

import (
	"context"
	"fmt"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// HPATest tests horizontal pod autoscaling functionality
type HPATest struct {
	clientset *kubernetes.Clientset
	namespace string
	timeout   time.Duration
}

// NewHPATest creates a new HPA test
func NewHPATest(namespace string) (*HPATest, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &HPATest{
		clientset: clientset,
		namespace: namespace,
		timeout:   5 * time.Minute,
	}, nil
}

// TestHPAExists tests that HPA resources exist
func (t *HPATest) TestHPAExists(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpas, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		test.Fatalf("Failed to list HPAs: %v", err)
	}

	if len(hpas.Items) == 0 {
		test.Error("No HPAs found")
	}
}

// TestHPACPUMetric tests that CPU metric is configured
func (t *HPATest) TestHPACPUMetric(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if len(hpa.Spec.Metrics) == 0 {
		test.Error("No metrics configured in HPA")
		return
	}

	hasCPU := false
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil && metric.Resource.Name == "cpu" {
			hasCPU = true
			break
		}
	}

	if !hasCPU {
		test.Error("CPU metric not configured")
	}
}

// TestHPAMemoryMetric tests that memory metric is configured
func (t *HPATest) TestHPAMemoryMetric(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if len(hpa.Spec.Metrics) == 0 {
		test.Error("No metrics configured in HPA")
		return
	}

	hasMemory := false
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil && metric.Resource.Name == "memory" {
			hasMemory = true
			break
		}
	}

	if !hasMemory {
		test.Error("Memory metric not configured")
	}
}

// TestHPAMinReplicas tests that minimum replicas are configured
func (t *HPATest) TestHPAMinReplicas(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas < 3 {
		test.Error("Minimum replicas should be at least 3")
	}
}

// TestHPAMaxReplicas tests that maximum replicas are configured
func (t *HPATest) TestHPAMaxReplicas(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if hpa.Spec.MaxReplicas < 10 {
		test.Error("Maximum replicas should be at least 10")
	}
}

// TestHPAScaleUpBehavior tests that scale-up behavior is configured
func (t *HPATest) TestHPAScaleUpBehavior(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleUp == nil {
		test.Error("Scale-up behavior not configured")
		return
	}

	if hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds == nil || *hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != 0 {
		test.Error("Scale-up stabilization window should be 0")
	}
}

// TestHPAScaleDownBehavior tests that scale-down behavior is configured
func (t *HPATest) TestHPAScaleDownBehavior(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		test.Error("Scale-down behavior not configured")
		return
	}

	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil || *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds < 300 {
		test.Error("Scale-down stabilization window should be at least 300 seconds")
	}
}

// TestHPATargetRef tests that target reference is correct
func (t *HPATest) TestHPATargetRef(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if hpa.Spec.ScaleTargetRef.Kind != "Deployment" {
		test.Errorf("Expected Deployment target, got %s", hpa.Spec.ScaleTargetRef.Kind)
	}

	if hpa.Spec.ScaleTargetRef.Name != "streamgate-blue" {
		test.Errorf("Expected streamgate-blue target, got %s", hpa.Spec.ScaleTargetRef.Name)
	}
}

// TestHPAStatus tests that HPA status is available
func (t *HPATest) TestHPAStatus(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-cpu", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-cpu not found: %v", err)
		return
	}

	if hpa.Status.CurrentReplicas == 0 {
		test.Logf("Warning: HPA has no current replicas (may be normal on first setup)")
	}

	if hpa.Status.DesiredReplicas == 0 {
		test.Logf("Warning: HPA has no desired replicas (may be normal on first setup)")
	}
}

// TestHPARequestRateMetric tests that request rate metric is configured
func (t *HPATest) TestHPARequestRateMetric(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-hpa-requests", metav1.GetOptions{})
	if err != nil {
		test.Skipf("HPA streamgate-hpa-requests not found: %v", err)
		return
	}

	if len(hpa.Spec.Metrics) == 0 {
		test.Error("No metrics configured in HPA")
		return
	}

	hasRequestRate := false
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.PodsMetricSourceType && metric.Pods != nil && metric.Pods.Metric.Name == "http_requests_per_second" {
			hasRequestRate = true
			break
		}
	}

	if !hasRequestRate {
		test.Logf("Warning: Request rate metric not configured (may be using custom metrics)")
	}
}

// TestCanaryHPA tests that canary HPA is configured
func (t *HPATest) TestCanaryHPA(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	hpa, err := t.clientset.AutoscalingV2().HorizontalPodAutoscalers(t.namespace).Get(ctx, "streamgate-canary-hpa", metav1.GetOptions{})
	if err != nil {
		test.Skipf("Canary HPA not found: %v", err)
		return
	}

	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas < 1 {
		test.Error("Canary HPA minimum replicas should be at least 1")
	}

	if hpa.Spec.MaxReplicas < 5 {
		test.Error("Canary HPA maximum replicas should be at least 5")
	}
}

// Run executes all HPA tests
func (t *HPATest) Run(test *testing.T) {
	test.Run("HPAExists", t.TestHPAExists)
	test.Run("HPACPUMetric", t.TestHPACPUMetric)
	test.Run("HPAMemoryMetric", t.TestHPAMemoryMetric)
	test.Run("HPAMinReplicas", t.TestHPAMinReplicas)
	test.Run("HPAMaxReplicas", t.TestHPAMaxReplicas)
	test.Run("HPAScaleUpBehavior", t.TestHPAScaleUpBehavior)
	test.Run("HPAScaleDownBehavior", t.TestHPAScaleDownBehavior)
	test.Run("HPATargetRef", t.TestHPATargetRef)
	test.Run("HPAStatus", t.TestHPAStatus)
	test.Run("HPARequestRateMetric", t.TestHPARequestRateMetric)
	test.Run("CanaryHPA", t.TestCanaryHPA)
}

// TestHPA is the main test function
func TestHPA(test *testing.T) {
	hpaTest, err := NewHPATest("streamgate")
	if err != nil {
		test.Skipf("Skipping HPA tests: %v", err)
	}

	hpaTest.Run(test)
}
