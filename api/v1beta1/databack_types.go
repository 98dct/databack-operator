/*
Copyright 2024.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type Origin struct {
	Host string `json:"host"`
	Port int32  `json:"port"`
	//用户名和密码放在secret中
	Username string `json:"username"`
	Password string `json:"password"`
}

type Destination struct {
	EndPoint     string `json:"endPoint"`
	AccessKey    string `json:"accessKey"`
	AccessSecret string `json:"accessSecret"`
	BucketName   string `json:"bucketName"`
}

// DatabackSpec defines the desired state of Databack
type DatabackSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//是否开启备份任务
	Enable bool `json:"enable"`
	//备份时间
	StartTime string `json:"startTime"`
	//间隔周期(分钟)
	Period int `json:"period"`
	//数据源
	Origin Origin `json:"origin"`
	//备份的目标地址
	Destination Destination `json:"destination"`
}

// DatabackStatus defines the observed state of Databack
type DatabackStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Active           bool   `json:"active"`
	NexTime          int64  `json:"nexTime"`
	LastBackupResult string `json:"LastBackupResult"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Databack is the Schema for the databacks API
type Databack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabackSpec   `json:"spec,omitempty"`
	Status DatabackStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DatabackList contains a list of Databack
type DatabackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Databack `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Databack{}, &DatabackList{})
}
