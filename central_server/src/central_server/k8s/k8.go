package k8s

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

/*
Outside of this file
1. Modify chunk servers to run in headless mode so they can all be accessed seperately   v/
2. Set up port forwarding for central server, and all chunk server ports - aiming to port forward central server to 6440 (which will be open on the server), and chunk servers to something in the range 36000-37000
3. Use caddy api to reverse proxy eg x-1y1.chunk1.clairegregg.com to eg port forwarded localhost:36101    X  using dynamic routing instead
4. Write/read database with specific chunk urls for each coordinate - eg x-1y1.chunk1.clairegregg.com

In this file
1. Get clients for kubernetes clusters 									v/
2. Check each cluster for how many replicas of the server it has 		v/
3. Bring up new cluster when requested									v/
*/

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
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	if err != nil {
		return 0, err
	}
	return len(pods.Items), nil
}

func NewChunkServer(ctx context.Context, clients []*ClusterClient) (string, error) {
	// Get count of number of chunk servers in each cluster, setting the count to MaxInt if the cluster is unreachable.
	for _, client := range clients {
		count, err := checkChunkCount(ctx, client.clientset)
		if err != nil {
			client.currentCount = count
			fmt.Printf("Failed to get chunk server count for cluster %s: %v\n", client.clusterName, err)
		} else {
			client.currentCount = math.MaxInt
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

	clientset := clients[0].clientset

	// Add another replica to the cluster
	scale, err := clientset.AppsV1().StatefulSets("").GetScale(ctx, "pacman-chunk", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	scale.Spec.Replicas = scale.Spec.Replicas + 1
	_, err = clientset.AppsV1().StatefulSets("").UpdateScale(ctx, "pacman-chunk", scale, metav1.UpdateOptions{})
	if err != nil {
		return "", err
	}

	// Retrieve the name of the new chunk server/replica
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	podList := pods.Items
	slices.SortFunc(podList, func(a, b corev1.Pod) int {
		return a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time)
	})
	newestPod := podList[len(podList)-1]

	return getChunkServerUrl(newestPod.Name, clients[0].clusterName), nil
}

func getChunkServerUrl(podName, clusterName string) string {
	// Construct the url for the new pod - clustername.clairegregg.com/?id=chunkservernumber
	// Pods are named like pacman-chunk-1
	splitPodName := strings.Split(podName, "-")
	podNum := splitPodName[len(splitPodName)-1]
	return fmt.Sprintf("%s.clairegregg.com/?id=%s", clusterName, podNum)
}

func GetCurrentChunkServerUrls(ctx context.Context, clients []*ClusterClient) ([]string, error){
	var urls []string

	// Search all clusters
	for _, client := range clients {
		clientset := client.clientset
		// Retrieve all pods
		pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
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