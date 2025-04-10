package k8s

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"slices"

	"cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"

	kruiseclientset "github.com/openkruise/kruise-api/client/clientset/versioned"
)

// ClusterClient represents a Kubernetes cluster client.
type ClusterClient struct {
	clientset    *kubernetes.Clientset
	kruiseclient *kruiseclientset.Clientset
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
		kruiseclient, err := kruiseclientset.NewForConfig(config)
		if err != nil {
			return nil, err
		}

		newClient := &ClusterClient{
			clientset:    clientset,
			kruiseclient: kruiseclient,
			clusterName:  currentContext,
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

func DeleteChunkServer(ctx context.Context, clients []*ClusterClient, x, y int, url string) error {
	clusterName := strings.Split(url, ".")[0] // Extract just (EG) chunk1 from chunk1.clairegregg.com/?id=2
	chunkId := strings.Split(url, "=")[1]     // Extract just (EG) 2 from chunk1.clairegregg.com/?id=2

	// Get correct cluster client
	var clientset *kubernetes.Clientset
	var kruiseclient *kruiseclientset.Clientset
	for _, client := range clients {
		log.Printf("Cluster name is %v", client.clusterName)
		if (client.clusterName == clusterName) || (client.clusterName == "kind-"+clusterName) {
			kruiseclient = client.kruiseclient
			clientset = client.clientset
			break
		}
	}
	if clientset == nil || kruiseclient == nil {
		return fmt.Errorf("no clients match cluster name %v", clusterName)
	}

	// Get statefulset, remove one from replicas, and reserve the ordinal (pod number) until it is needed again
	statefulset, err := kruiseclient.AppsV1beta1().StatefulSets("default").Get(ctx, "pacman-chunk", metav1.GetOptions{})
	if err != nil {
		return err
	}
	statefulset.Spec.Replicas = ptr.To((*(statefulset.Spec.Replicas) - 1))
	podOrdinal, err := strconv.Atoi(chunkId)
	if err != nil {
		return fmt.Errorf("Unable to retrieve pod ordinal for chunkID %v: %w", chunkId, err)
	}
	statefulset.Spec.ReserveOrdinals = append(statefulset.Spec.ReserveOrdinals, podOrdinal) // Make sure, eg pacman-chunk-7 isn't immediately recreated

	// Update StatefulSet 
	statefulset, err = kruiseclient.AppsV1beta1().StatefulSets("default").Update(ctx, statefulset, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	
	return nil
}

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

	log.Printf("Adding chunk server to cluster %s", clients[0].clusterName)
	clientset := clients[0].clientset
	kruiseclient := clients[0].kruiseclient

	// Get current state of the cluster
	podList, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	resourceVersion := podList.ResourceVersion

	// Add another replica to the cluster
	statefulset, err := kruiseclient.AppsV1beta1().StatefulSets("default").Get(ctx, "pacman-chunk", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	statefulset.Spec.Replicas = ptr.To((*(statefulset.Spec.Replicas) + 1))
	if len(statefulset.Spec.ReserveOrdinals) > 0 { // Allow recreating of the oldest deleted pod if applicable
		statefulset.Spec.ReserveOrdinals = statefulset.Spec.ReserveOrdinals[1:len(statefulset.Spec.ReserveOrdinals)]
	}
	statefulset, err = kruiseclient.AppsV1beta1().StatefulSets("default").Update(ctx, statefulset, metav1.UpdateOptions{})
	if err != nil {
		return "", err
	}

	// Wait until the new pod has started
	watcher, err := clientset.CoreV1().Pods("default").Watch(ctx, metav1.ListOptions{
		LabelSelector:   "app=pacman-chunk",
		ResourceVersion: resourceVersion,
	})
	if err != nil {
		return "", err
	}
	defer watcher.Stop()
	for event := range watcher.ResultChan() {
		if event.Type == watch.Added {
			pod := event.Object.(*corev1.Pod)
			log.Printf("Created pod with name %s, status %v", pod.Name, pod.Status)
			break
		}
		log.Printf("Nothing created, event type %v", event.Type)
	}

	// List all pods for the StatefulSet.
	pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	ps := pods.Items
	slices.SortFunc(ps, func(a, b corev1.Pod) int {
		return a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time)
	})

	// // Retrieve container name dynamically (assumes first container is the target).
	// containerName := ps[len(ps)-1].Spec.Containers[0].Name

	// // Create a coordinate string (e.g., "100,200").
	// coordinate := fmt.Sprintf("%d,%d", x, y)

	// // Patch the pod to add/update an environment variable "CHUNK_COORDINATE".
	// patch := []byte(fmt.Sprintf(`{"spec": {"containers": [{"name": "%s", "env": [{"name": "CHUNK_COORDINATE", "value": "%s"}]}]}}`, containerName, coordinate))
	// _, err = clientset.CoreV1().Pods("default").Patch(ctx, ps[len(ps)-1].Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	// if err != nil {
	// 	return "", fmt.Errorf("failed to patch pod with coordinate env: %w", err)
	// }

	// Construct and return the chunk server URL.
	return getChunkServerUrl(ps[len(ps)-1].Name, clients[0].clusterName), nil
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