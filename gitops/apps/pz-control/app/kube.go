package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// PodStatus holds the summary state of the game-server pod.
type PodStatus struct {
	Name      string    `json:"name"`
	Phase     string    `json:"phase"`
	Ready     bool      `json:"ready"`
	StartedAt time.Time `json:"started_at"`
	Restarts  int32     `json:"restarts"`
}

// KubeClient is the interface the HTTP handlers depend on.
// The real implementation talks to in-cluster Kubernetes; tests use a fake.
type KubeClient interface {
	PodStatus(ctx context.Context) (PodStatus, error)
	PodLogs(ctx context.Context, tail int64) (string, error)
	RestartDeployment(ctx context.Context) error
}

type kubeClient struct {
	cs         kubernetes.Interface
	namespace  string
	deployment string
	podLabel   string // label selector, e.g. "app=project-zomboid"
}

// NewKubeClient builds a real KubeClient from the in-cluster service-account config.
func NewKubeClient(namespace, deployment, podLabel string) (KubeClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("clientset: %w", err)
	}
	return &kubeClient{cs: cs, namespace: namespace, deployment: deployment, podLabel: podLabel}, nil
}

// currentPod returns the single pod matching the label selector.
// The deployment uses replicas:1 + strategy:Recreate so there is at most one.
func (k *kubeClient) currentPod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := k.cs.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{LabelSelector: k.podLabel})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods matching %q in %q", k.podLabel, k.namespace)
	}
	return &pods.Items[0], nil
}

// PodStatus returns current phase, readiness, restart count and start time.
func (k *kubeClient) PodStatus(ctx context.Context) (PodStatus, error) {
	pod, err := k.currentPod(ctx)
	if err != nil {
		return PodStatus{}, err
	}
	s := PodStatus{
		Name:  pod.Name,
		Phase: string(pod.Status.Phase),
	}
	if pod.Status.StartTime != nil {
		s.StartedAt = pod.Status.StartTime.Time
	}
	for _, c := range pod.Status.ContainerStatuses {
		s.Restarts += c.RestartCount
		if c.Ready {
			s.Ready = true
		} else {
			s.Ready = false
			break
		}
	}
	return s, nil
}

// PodLogs returns the last `tail` lines of logs from the current pod.
func (k *kubeClient) PodLogs(ctx context.Context, tail int64) (string, error) {
	pod, err := k.currentPod(ctx)
	if err != nil {
		return "", err
	}
	req := k.cs.CoreV1().Pods(k.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{TailLines: &tail})
	rc, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// RestartDeployment triggers a rolling restart by patching
// .spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"].
// This is the same mechanism used by `kubectl rollout restart`.
func (k *kubeClient) RestartDeployment(ctx context.Context) error {
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = k.cs.AppsV1().Deployments(k.namespace).Patch(
		ctx,
		k.deployment,
		types.StrategicMergePatchType,
		body,
		metav1.PatchOptions{},
	)
	return err
}
