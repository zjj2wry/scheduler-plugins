package stateful

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"
)

func TestFilter(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	informers := informers.NewSharedInformerFactory(clientset, 0)
	statefulsetInformer := informers.Apps().V1().StatefulSets()
	statefulsetLister := statefulsetInformer.Lister()
	stableSchedule := &Stable{
		statefulSetLister: statefulsetLister,
		clientset:         clientset,
	}
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "n1",
			Annotations: map[string]string{
				"statefulset-stable.scheduling.sigs.k8s.io/record": `{"Records":{"web-0":"node1"}}`,
			},
		},
	}
	err := statefulsetInformer.Informer().GetIndexer().Add(statefulset)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		pod      *corev1.Pod
		node     *corev1.Node
		expected framework.Code
	}{
		{
			name: "the pod is rescheduled to the node1",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-0",
					Namespace: "n1",
					Labels: map[string]string{
						"statefulset-stable.scheduling.sigs.k8s.io": "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "StatefulSet",
							Name: "web",
						},
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
			},
			expected: framework.Success,
		},
		{
			name: "pod web-0 unschedule to node2",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-0",
					Namespace: "n1",
					Labels: map[string]string{
						"statefulset-stable.scheduling.sigs.k8s.io": "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "StatefulSet",
							Name: "web",
						},
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
			},
			expected: framework.Unschedulable,
		},
		{
			name: "owner references are not statefulset",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-0",
					Namespace: "n1",
					Labels: map[string]string{
						"statefulset-stable.scheduling.sigs.k8s.io": "true",
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
			},
			expected: framework.Success,
		},
		{
			name: "pod has not statefulset-stable.scheduling.sigs.k8s.io=true label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-0",
					Namespace: "n1",
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
				},
			},
			expected: framework.Success,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeInfo := schedulernodeinfo.NewNodeInfo()
			err := nodeInfo.SetNode(tt.node)
			if err != nil {
				t.Fatal(err)
			}
			res := stableSchedule.Filter(context.TODO(), nil, tt.pod, nodeInfo)
			if res.Code() != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, res.Code())

			}

		})
	}
}

func TestPostBind(t *testing.T) {
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "n1",
		},
	}

	clientset := fake.NewSimpleClientset(statefulset)
	informers := informers.NewSharedInformerFactory(clientset, 0)
	statefulsetInformer := informers.Apps().V1().StatefulSets()
	statefulsetLister := statefulsetInformer.Lister()
	stableSchedule := &Stable{
		statefulSetLister: statefulsetLister,
		clientset:         clientset,
	}

	err := statefulsetInformer.Informer().GetIndexer().Add(statefulset)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name                string
		pod                 *corev1.Pod
		nodeName            string
		expectedAnnotations map[string]string
	}{
		{
			name: "the pod scheduled to the node1, but owner references are not statefulset",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-0",
					Namespace: "n1",
					Labels: map[string]string{
						"statefulset-stable.scheduling.sigs.k8s.io": "true",
					},
				},
			},
			nodeName: "node1",
		},
		{
			name: "the pod scheduled to the node1, but pod has no statefulset-stable.scheduling.sigs.k8s.io=true label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-0",
					Namespace: "n1",
				},
			},
			nodeName: "node1",
		},
		{
			name: "the pod scheduled to the node1",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web-0",
					Namespace: "n1",
					Labels: map[string]string{
						"statefulset-stable.scheduling.sigs.k8s.io": "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "StatefulSet",
							Name: "web",
						},
					},
				},
			},
			nodeName: "node1",
			expectedAnnotations: map[string]string{
				"statefulset-stable.scheduling.sigs.k8s.io/record": `{"Records":{"web-0":"node1"}}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			stableSchedule.PostBind(ctx, nil, tt.pod, tt.nodeName)
			s, err := clientset.AppsV1().StatefulSets(statefulset.Namespace).Get(ctx, statefulset.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(tt.expectedAnnotations, s.Annotations) {
				t.Errorf("expected %v, got %v", tt.expectedAnnotations, s.Annotations)
			}
		})
	}
}
