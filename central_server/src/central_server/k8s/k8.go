package k8s

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type ClusterClient struct {
	clientset *kubernetes.Clientset
	clusterName string
	currentCount int
}

func KubeClients(kubeconfigs []string) ([]*ClusterClient, error) {
	var clients []*ClusterClient
	for _, kubeconfig := range kubeconfigs {
		rawConfig, err := clientcmd.LoadFromFile(kubeconfig)
		if err != nil {
			return nil, err
		}
		currentContext := rawConfig.CurrentContext
		strings.ReplaceAll(currentContext, "kind-", "") // Clusters are named like "kind-chunk1", but for URL construction we just want "chunk1"

		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, err
		}

		newClient := &ClusterClient{
			clientset: clientset,
			clusterName: currentContext,
		}

		clients = append(clients, newClient)
	}
	return clients, nil
}

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
	clusterName := strings.Split(url, ".")[0] // Extract just (EG) chunk1 from chunk1.clairegregg.com/?id=1
	chunkId := strings.Split(url, "=")[1]
	podName := "pacman-chunk-"+chunkId

	// Get correct cluster client
	var clientset *kubernetes.Clientset
	for _, client  := range clients {
		log.Printf("Cluster name is %v", client.clusterName)
		if (client.clusterName == clusterName) || (client.clusterName == "kind-"+clusterName) {
			clientset = client.clientset
			break
		}
	}
	if clientset == nil {
		return fmt.Errorf("no clients match cluster name %v", clusterName)
	}

	// Set the pod serving the server to have the lowest deletion cost, meaning it should be deleted first
	server, err := clientset.CoreV1().Pods("default").Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	ann := server.GetAnnotations()
	if ann == nil {
		ann = map[string]string{"controller.kubernetes.io/pod-deletion-cost": strconv.Itoa(math.MinInt)}
	} else {
		ann["controller.kubernetes.io/pod-deletion-cost"] = strconv.Itoa(math.MinInt) // Deletion cost is 0 by default, so min int should be fine!
	}
	server.SetAnnotations(ann)

	// Decrease number of replicas
	scale, err := clientset.AppsV1().StatefulSets("default").GetScale(ctx, "pacman-chunk", metav1.GetOptions{})
	if err != nil {
		return err
	}
	scale.Spec.Replicas = scale.Spec.Replicas - 1
	_, err = clientset.AppsV1().StatefulSets("default").UpdateScale(ctx, "pacman-chunk", scale, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func NewChunkServer(ctx context.Context, clients []*ClusterClient) (string, error) {
	// Get count of number of chunk servers in each cluster, setting the count to MaxInt if the cluster is unreachable.
	for _, client := range clients {
		count, err := checkChunkCount(ctx, client.clientset)
		if err != nil {
			client.currentCount = math.MaxInt
			fmt.Printf("Failed to get chunk server count for cluster %s: %v\n", client.clusterName, err)
		} else {
			client.currentCount = count
		}
	}

	// Sort the clusters by the number of chunk servers they contain
	slices.SortFunc(clients, func(a, b *ClusterClient) int {
		return cmp.Compare(a.currentCount, b.currentCount)
	})

	// If no clusters are reachable, return an error
	if clients[0].currentCount == math.MaxInt {
		return "", fmt.Errorf("No clusters are reachable")
	}

	log.Printf("Adding chunk server to cluster %s", clients[0].clusterName)
	clientset := clients[0].clientset

	// Get current state of the cluster
	podList, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	if err != nil {
		return "", err
	}
	resourceVersion := podList.ResourceVersion

	// Add another replica to the cluster
	scale, err := clientset.AppsV1().StatefulSets("default").GetScale(ctx, "pacman-chunk", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	scale.Spec.Replicas = scale.Spec.Replicas + 1
	_, err = clientset.AppsV1().StatefulSets("default").UpdateScale(ctx, "pacman-chunk", scale, metav1.UpdateOptions{})
	if err != nil {
		return "", err
	}

	// Wait until the new pod has started
	watcher, err := clientset.CoreV1().Pods("default").Watch(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
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

	// Retrieve the name of the new chunk server/replica
	pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	ps := pods.Items
	slices.SortFunc(ps, func(a, b corev1.Pod) int {
		return a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time)
	})
	newestPod := ps[len(ps)-1]

	return getChunkServerUrl(newestPod.Name, clients[0].clusterName), nil
}

func getChunkServerUrl(podName, clusterName string) string {
	// Construct the url for the new pod - clustername.clairegregg.com/?id=chunkservernumber
	// Pods are named like pacman-chunk-1
	splitPodName := strings.Split(podName, "-")
	podNum := splitPodName[len(splitPodName)-1]
	return fmt.Sprintf("%s.clairegregg.com/?id=%s", strings.ReplaceAll(clusterName, "kind-", ""), podNum)
}

func GetCurrentChunkServerUrls(ctx context.Context, clients []*ClusterClient) ([]string, error){
	var urls []string

	// Search all clusters
	for _, client := range clients {
		clientset := client.clientset
		// Retrieve all pods
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