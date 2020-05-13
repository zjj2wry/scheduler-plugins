/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package stateful

import (
	"context"
	"encoding/json"
	"log"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	statefulsetlisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/util/retry"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"
)

var _ framework.FilterPlugin = &Stable{}
var _ framework.PostBindPlugin = &Stable{}

// Name is the name of the plugin used in the plugin registry and configurations.
const (
	Name                    = "statefulset-stable"
	Kind                    = "StatefulSet"
	StatefulsetStableRecord = "statefulset-stable.scheduling.sigs.k8s.io/record"
	StatefulsetStable       = "statefulset-stable.scheduling.sigs.k8s.io"
)

// Stable is a plugin that implements statefulset stable schedule
type Stable struct {
	statefulSetLister statefulsetlisters.StatefulSetLister
	clientset         clientset.Interface
}

type ScheduleRecord struct {
	Records map[string]string
}

// Name returns name of the plugin.
func (st *Stable) Name() string {
	return Name
}

// New initializes a new plugin and returns it.
func New(_ *runtime.Unknown, handle framework.FrameworkHandle) (framework.Plugin, error) {
	statefulsetLister := handle.SharedInformerFactory().Apps().V1().StatefulSets().Lister()
	clientset := handle.ClientSet()
	return &Stable{
		statefulSetLister: statefulsetLister,
		clientset:         clientset,
	}, nil
}

// Filter checks whether the pod meets the current plugin conditions and
// restores the last scheduled record. Filters out unmatched nodes.
func (st *Stable) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *schedulernodeinfo.NodeInfo) *framework.Status {
	if !containStatefulsetStableLabel(pod) {
		return framework.NewStatus(framework.Success, "")
	}
	if statefulset := st.createByStatefulset(pod); statefulset != nil {
		// try get the pod schedule record
		record, err := getScheduleRecord(statefulset)
		if err != nil {
			return framework.NewStatus(framework.Unschedulable, err.Error())
		}
		if record != nil {
			if node, ok := record.Records[pod.GetName()]; ok {
				// want to schedule to the original node, if the node is different, filter directly
				if node != nodeInfo.Node().GetName() {
					return framework.NewStatus(framework.Unschedulable, "")
				}
			}
		}
	}
	return framework.NewStatus(framework.Success, "")
}

// PostBind record the result of the current schedule to the annotation of statefulset
func (st *Stable) PostBind(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	if !containStatefulsetStableLabel(pod) {
		return
	}
	// although the updates of the pods created by the statefulset are ordered and
	// can relieve the problem of concurrent updates, but the update operation cannot guarantee success,
	// should catch error and add retry.
	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if statefulset := st.createByStatefulset(pod); statefulset != nil {
			return st.setScheduleRecord(ctx, statefulset, pod, nodeName)
		}
		return nil
	})
	if retryErr != nil {
		log.Printf("Failed to record scheduling result: %v\n", retryErr)
	}
}

func containStatefulsetStableLabel(pod *v1.Pod) bool {
	label := pod.GetLabels()
	if label == nil {
		return false
	}
	if label[StatefulsetStable] == "true" {
		return true
	}
	return false
}

// createByStatefulset check if the pod belongs to statefulset, if yes, return statefulset object
func (st *Stable) createByStatefulset(pod *v1.Pod) *appsv1.StatefulSet {
	ows := pod.GetOwnerReferences()
	for _, ow := range ows {
		if ow.Kind == Kind {
			statefulset, err := st.statefulSetLister.StatefulSets(pod.Namespace).Get(ows[0].Name)
			if err != nil {
				return nil
			}
			return statefulset
		}
	}
	return nil
}

func getScheduleRecord(statefulset *appsv1.StatefulSet) (*ScheduleRecord, error) {
	var record *ScheduleRecord
	var err error
	ats := statefulset.GetAnnotations()
	if ats != nil {
		if rec, ok := ats[StatefulsetStableRecord]; ok {
			if err := json.Unmarshal([]byte(rec), &record); err != nil {
				return nil, err
			}
		}
	}
	return record, err
}

func (st *Stable) setScheduleRecord(ctx context.Context, statefulset *appsv1.StatefulSet, pod *v1.Pod, nodeName string) error {
	needUpdate := false
	record, err := getScheduleRecord(statefulset)
	if err != nil {
		return err
	}
	if record == nil {
		record = new(ScheduleRecord)
	}

	if record.Records == nil {
		record.Records = make(map[string]string)
	}

	if _, ok := record.Records[pod.GetName()]; !ok {
		record.Records[pod.GetName()] = nodeName
		needUpdate = true
	}

	if needUpdate {
		statefulsetCopy := statefulset.DeepCopy()
		recordBytes, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if statefulsetCopy.Annotations == nil {
			statefulsetCopy.Annotations = make(map[string]string)
		}
		statefulsetCopy.Annotations[StatefulsetStableRecord] = string(recordBytes)
		_, err = st.clientset.AppsV1().StatefulSets(statefulset.Namespace).Update(ctx, statefulsetCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}
