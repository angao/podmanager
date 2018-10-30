/*
Copyright 2017 The Kubernetes Authors.

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

package util

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	extensionsv1alpha1 "github.com/angao/podmanager/pkg/apis/extensions/v1alpha1"
	apps "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	controllerUID  = "123"
	controllerUID1 = "456"
)

func newReplicaSet(replicas int, owner metav1.Object, t *metav1.Time) *apps.ReplicaSet {
	rs := &apps.ReplicaSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foobar",
			Namespace:         metav1.NamespaceDefault,
			ResourceVersion:   "18",
			DeletionTimestamp: t,
		},
		Spec: apps.ReplicaSetSpec{
			Replicas: func() *int32 { i := int32(replicas); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"type": "production"}},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"name": "foo",
						"type": "production",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image:                  "foo/bar",
							TerminationMessagePath: v1.TerminationMessagePathDefault,
							ImagePullPolicy:        v1.PullIfNotPresent,
						},
					},
					RestartPolicy: v1.RestartPolicyAlways,
					DNSPolicy:     v1.DNSDefault,
					NodeSelector: map[string]string{
						"baz": "blah",
					},
				},
			},
		},
	}
	if owner != nil {
		rs.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(owner, apps.SchemeGroupVersion.WithKind("Fake"))}
	}
	return rs
}

func newReplicaSetForTest(replicas int, name string, createTimestamp metav1.Time) *apps.ReplicaSet {
	rs := &apps.ReplicaSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         metav1.NamespaceDefault,
			ResourceVersion:   "18",
			CreationTimestamp: createTimestamp,
		},
		Spec: apps.ReplicaSetSpec{
			Replicas: func() *int32 { i := int32(replicas); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"type": "production"}},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"name": "foo",
						"type": "production",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image:                  "foo/bar",
							TerminationMessagePath: v1.TerminationMessagePathDefault,
							ImagePullPolicy:        v1.PullIfNotPresent,
						},
					},
					RestartPolicy: v1.RestartPolicyAlways,
					DNSPolicy:     v1.DNSDefault,
					NodeSelector: map[string]string{
						"baz": "blah",
					},
				},
			},
		},
	}
	return rs
}

func TestClaimReplicaSets(t *testing.T) {
	// controllerKind := schema.GroupVersionKind{}
	type test struct {
		name     string
		manager  *ReplicaSetControllerRefManager
		replicas []*apps.ReplicaSet
		claimed  []*apps.ReplicaSet
	}
	var tests = []test{
		{
			name:     "Claim replicaset with correct label",
			manager:  NewReplicaSetControllerRefManager(&apps.Deployment{}, func() error { return nil }),
			replicas: []*apps.ReplicaSet{newReplicaSet(10, nil, nil)},
			claimed:  []*apps.ReplicaSet{newReplicaSet(10, nil, nil)},
		},
		func() test {
			d1 := apps.Deployment{}
			d1.UID = types.UID(controllerUID)
			now := metav1.Now()
			d1.DeletionTimestamp = &now

			d2 := apps.Deployment{}
			d2.UID = types.UID(controllerUID1)
			return test{
				name: "Controller deletion can not claim replicaset",
				manager: NewReplicaSetControllerRefManager(&d1,
					func() error { return nil }),
				replicas: []*apps.ReplicaSet{newReplicaSet(10, &d1, nil), newReplicaSet(10, &d2, nil)},
				claimed:  nil,
			}
		}(),
		func() test {
			d1 := apps.Deployment{}
			d1.UID = types.UID(controllerUID)

			d2 := apps.Deployment{}
			d2.UID = types.UID(controllerUID1)
			return test{
				name: "Test for controllerRef",
				manager: NewReplicaSetControllerRefManager(&d1,
					func() error { return nil }),
				replicas: []*apps.ReplicaSet{newReplicaSet(10, &d1, nil), newReplicaSet(10, &d2, nil)},
				claimed:  []*apps.ReplicaSet{newReplicaSet(10, &d1, nil)},
			}
		}(),
		func() test {
			d1 := apps.Deployment{}
			d1.UID = types.UID(controllerUID)

			return test{
				name: "Test for replicaset deletion",
				manager: NewReplicaSetControllerRefManager(&d1,
					func() error { return nil }),
				replicas: []*apps.ReplicaSet{newReplicaSet(10, &d1, nil), newReplicaSet(10, nil, &metav1.Time{Time: time.Now()})},
				claimed:  []*apps.ReplicaSet{newReplicaSet(10, &d1, nil)},
			}
		}(),
		func() test {
			d1 := apps.Deployment{}
			d1.UID = types.UID(controllerUID)
			now := metav1.Now()
			d1.DeletionTimestamp = &now

			return test{
				name: "Test for controller deletion",
				manager: NewReplicaSetControllerRefManager(&d1,
					func() error { return nil }),
				replicas: []*apps.ReplicaSet{newReplicaSet(10, nil, nil)},
				claimed:  nil,
			}
		}(),
		func() test {
			d1 := apps.Deployment{}
			d1.UID = types.UID(controllerUID)

			return test{
				name: "Test for CanAdoptFunc not empty",
				manager: NewReplicaSetControllerRefManager(&d1,
					RecheckDeletionTimestamp(func() (metav1.Object, error) {
						return &d1, nil
					})),
				replicas: []*apps.ReplicaSet{newReplicaSet(10, nil, nil)},
				claimed:  []*apps.ReplicaSet{newReplicaSet(10, nil, nil)},
			}
		}(),
		func() test {
			d1 := apps.Deployment{}
			d1.UID = types.UID(controllerUID)

			return test{
				name: "Test for CanAdoptFunc not empty",
				manager: NewReplicaSetControllerRefManager(&d1,
					RecheckDeletionTimestamp(func() (metav1.Object, error) {
						return nil, errors.New("deployment has be deleted")
					})),
				replicas: []*apps.ReplicaSet{newReplicaSet(10, nil, nil)},
				claimed:  nil,
			}
		}(),
		func() test {
			d1 := apps.Deployment{}
			d1.UID = types.UID(controllerUID)

			return test{
				name: "Test for CanAdoptFunc not empty",
				manager: NewReplicaSetControllerRefManager(&d1,
					RecheckDeletionTimestamp(func() (metav1.Object, error) {
						now := metav1.Now()
						d1.DeletionTimestamp = &now
						return &d1, nil
					})),
				replicas: []*apps.ReplicaSet{newReplicaSet(10, nil, nil)},
				claimed:  nil,
			}
		}(),
	}
	for _, test := range tests {
		claimed, err := test.manager.ClaimReplicaSets(test.replicas)
		if err != nil {
			errMsg := "can't adopt ReplicaSet default/foobar"
			if !strings.Contains(err.Error(), errMsg) {
				t.Errorf("Test case `%s`, unexpected error: %v", test.name, err)
			}
		} else if !reflect.DeepEqual(test.claimed, claimed) {
			t.Errorf("Test case `%s`, claimed wrong pods. Expected %v, got %v", test.name, replicasetToStringSlice(test.claimed), replicasetToStringSlice(claimed))
		}

	}
}

func TestReplicaSetsByCreationTimestamp(t *testing.T) {
	t1, _ := time.Parse("2006-01-02 15:04:05", "2018-09-11 12:00:00")
	t2, _ := time.Parse("2006-01-02 15:04:05", "2018-09-11 12:01:00")
	t3, _ := time.Parse("2006-01-02 15:04:05", "2018-09-11 12:02:00")

	replicasets := []*apps.ReplicaSet{
		newReplicaSetForTest(10, "foo2", metav1.Time{Time: t2}),
		newReplicaSetForTest(10, "foo1", metav1.Time{Time: t1}),
		newReplicaSetForTest(10, "foo3", metav1.Time{Time: t3}),
		newReplicaSetForTest(10, "foo4", metav1.Time{Time: t1}),
	}
	sort.Sort(ReplicaSetsByCreationTimestamp(replicasets))

	expected := "foo1,foo4,foo2,foo3,"
	var claimed string
	for i := 0; i < len(replicasets); i++ {
		claimed += replicasets[i].Name + ","
	}

	if expected != claimed {
		t.Errorf("Expected %v, got %v", expected, claimed)
	}
}

func replicasetToStringSlice(replicasets []*apps.ReplicaSet) []string {
	var names []string
	for _, replicaset := range replicasets {
		names = append(names, replicaset.Name)
	}
	return names
}

func TestValidateIP4(t *testing.T) {
	type test struct {
		name     string
		ipSet    extensionsv1alpha1.IPSet
		expected bool
	}
	tests := []test{
		{
			name:     "ipset is empty",
			ipSet:    []string{},
			expected: false,
		},
		{
			name: "ipset incorrect",
			ipSet: []string{
				"192.168.56.1",
				"192.168.56.2",
				"19.168.56.11234",
			},
			expected: false,
		},
		{
			name: "ipset incorrect",
			ipSet: []string{
				"192.168.56.1",
				"192.168.56.2",
				"19168.56.11234",
			},
			expected: false,
		},
		{
			name: "ipset correct",
			ipSet: []string{
				"192.168.56.1",
				"192.168.56.2",
			},
			expected: true,
		},
	}

	for _, test := range tests {
		b, _ := ValidateIP4(test.ipSet)
		if b != test.expected {
			t.Errorf("Test case %s. Expected %v, got %v", test.name, test.expected, b)
		}
	}
}
