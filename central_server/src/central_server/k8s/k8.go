package k8s

import (
	"context"
	"fmt"
	"math"
	"strings"
	"slices"

	"cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types" // added import for patch type
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ClusterClient represents a Kubernetes cluster client.
type ClusterClient struct {
	clientset    *kubernetes.Clientset
	clusterName  string
	currentCount int
}

// KubeClients creates ClusterClient instances for each provided kubeconfig.
func KubeClients(kubeconfigs []string) ([]*ClusterClient, error) {
	var clients []*ClusterClient
	for _, kubeconfig := range kubeconfigs {
		rawConfig, err := clientcmd.LoadFromFile(kubeconfig)
		if err != nil {
			return nil, err
		}
		currentContext := rawConfig.CurrentContext
		// Remove "kind-" prefix for URL construction if present.
		currentContext = strings.ReplaceAll(currentContext, "kind-", "")

		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, err
		}

		newClient := &ClusterClient{
			clientset:   clientset,
			clusterName: currentContext,
		}
		clients = append(clients, newClient)
	}
	return clients, nil
}

// checkChunkCount returns the number of pods matching the "app=pacman-chunk" label.
func checkChunkCount(ctx context.Context, clientset *kubernetes.Clientset) (int, error) {
	pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	if err != nil {
		return 0, err
	}
	return len(pods.Items), nil
}

// NewChunkServer scales up the StatefulSet, selects the newest pod, patches it with a CHUNK_COORDINATE
// environment variable (set to the provided x,y coordinate), and returns the constructed chunk server URL.
func NewChunkServer(ctx context.Context, clients []*ClusterClient, x, y int) (string, error) {
	// Update each cluster client's count; if unreachable, set to MaxInt.
	for _, client := range clients {
		count, err := checkChunkCount(ctx, client.clientset)
		if err != nil {
			client.currentCount = math.MaxInt
			fmt.Printf("Failed to get chunk server count for cluster %s: %v\n", client.clusterName, err)
		} else {
			client.currentCount = count
		}
	}

	// Sort clusters by currentCount (lowest first).
	slices.SortFunc(clients, func(a, b *ClusterClient) int {
		return cmp.Compare(a.currentCount, b.currentCount)
	})

	// If the best cluster is unreachable, return an error.
	if clients[0].currentCount == math.MaxInt {
		return "", fmt.Errorf("No clusters are reachable")
	}

	clientset := clients[0].clientset

	// Scale up the StatefulSet "pacman-chunk" in the "default" namespace.
	scale, err := clientset.AppsV1().StatefulSets("default").GetScale(ctx, "pacman-chunk", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	scale.Spec.Replicas = scale.Spec.Replicas + 1
	_, err = clientset.AppsV1().StatefulSets("default").UpdateScale(ctx, "pacman-chunk", scale, metav1.UpdateOptions{})
	if err != nil {
		return "", err
	}

	// List all pods for the StatefulSet.
	pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	if err != nil {
		return "", err
	}

	// Sort pods by creation timestamp and select the newest one.
	podList := pods.Items
	slices.SortFunc(podList, func(a, b corev1.Pod) int {
		return a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time)
	})
	newestPod := podList[len(podList)-1]

	// Retrieve container name dynamically (assumes first container is the target).
	containerName := newestPod.Spec.Containers[0].Name

	// Create a coordinate string (e.g., "100,200").
	coordinate := fmt.Sprintf("%d,%d", x, y)

	// Patch the pod to add/update an environment variable "CHUNK_COORDINATE".
	patch := []byte(fmt.Sprintf(`{"spec": {"containers": [{"name": "%s", "env": [{"name": "CHUNK_COORDINATE", "value": "%s"}]}]}}`, containerName, coordinate))
	_, err = clientset.CoreV1().Pods("default").Patch(ctx, newestPod.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to patch pod with coordinate env: %w", err)
	}

	// Construct and return the chunk server URL.
	return getChunkServerUrl(newestPod.Name, clients[0].clusterName), nil
}

// getChunkServerUrl constructs a URL for a chunk server pod using its name and cluster name.
func getChunkServerUrl(podName, clusterName string) string {
	// Example: If podName is "pacman-chunk-1", split and take the last part as the server number.
	splitPodName := strings.Split(podName, "-")
	podNum := splitPodName[len(splitPodName)-1]
	return fmt.Sprintf("%s.clairegregg.com/?id=%s", strings.ReplaceAll(clusterName, "kind-", ""), podNum)
}

// GetCurrentChunkServerUrls returns the URLs of all chunk server pods across clusters.
func GetCurrentChunkServerUrls(ctx context.Context, clients []*ClusterClient) ([]string, error) {
	var urls []string

	// Iterate over all clusters.
	for _, client := range clients {
		clientset := client.clientset
		pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
			LabelSelector: "app=pacman-chunk",
		})
		if err != nil {
			return nil, err
		}

		for _, pod := range pods.Items {
			url := getChunkServerUrl(pod.Name, client.clusterName)
			urls = append(urls, url)
		}
	}
	return urls, nil
}