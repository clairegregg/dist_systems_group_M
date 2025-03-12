package k8s

import (
	"context"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/*
Outside of this file
1. Modify chunk servers to run in headless mode so they can all be accessed seperately   v/
2. Set up port forwarding for central server, and all chunk server ports - aiming to port forward central server to 6440 (which will be open on the server), and chunk servers to something in the range 36000-37000
3. Use caddy api to reverse proxy eg x-1y1.chunk1.clairegregg.com to eg port forwarded localhost:36101    X  using dynamic routing instead
4. Write/read database with specific chunk urls for each coordinate - eg x-1y1.chunk1.clairegregg.com

In this file
1. Get clients for kubernetes clusters 									v/
2. Check each cluster for how many replicas of the server it has
3. Bring up new cluster when requested
*/

func KubeClients(kubeconfigs []string) (error, []*kubernetes.Clientset) {
	var clientsets []*kubernetes.Clientset
	for _, kubeconfig := range kubeconfigs {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err, nil
		}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			return err, nil
		}

		clientsets = append(clientsets, clientset)
	}
	return nil, clientsets
}

func checkChunkCount(ctx context.Context, clientset *kubernetes.Clientset) (error, int) {
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=pacman-chunk",
	})
	if err != nil {
		return err, 0
	}
	return nil, len(pods.Items)
}