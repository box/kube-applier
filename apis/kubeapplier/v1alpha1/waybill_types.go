package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WaybillSpec defines the desired state of Waybill
type WaybillSpec struct {
	// AutoApply determines whether this Waybill will be automatically applied
	// by scheduled or polling runs.
	// +optional
	// +kubebuilder:default=true
	AutoApply *bool `json:"autoApply,omitempty"`

	// DelegateServiceAccountSecretRef references a Secret of type
	// kubernetes.io/service-account-token in the same namespace as the Waybill
	// that will be passed by kube-applier to kubectl when performing apply
	// runs.
	// +optional
	// +kubebuilder:default=kube-applier-delegate-token
	// +kubebuilder:validation:MinLength=1
	DelegateServiceAccountSecretRef string `json:"delegateServiceAccountSecretRef,omitempty"`

	// DryRun enables the dry-run flag when applying this Waybill.
	// +optional
	// +kubebuilder:default=false
	DryRun bool `json:"dryRun,omitempty"`

	// GitSSHSecretRef references a Secret that contains an item named `key` and
	// optionally an item named `known_hosts`. If present, these are passed to
	// the apply runtime and are used by `kustomize` when cloning remote bases.
	// This allows the use of bases from private repositories.
	// +optional
	GitSSHSecretRef *ObjectReference `json:"gitSSHSecretRef,omitempty"`

	// Prune determines whether pruning is enabled for this Waybill.
	// +optional
	// +kubebuilder:default=true
	Prune *bool `json:"prune,omitempty"`

	// PruneClusterResources determines whether pruning is enabled for cluster
	// resources, as part of this Waybill.
	// +optional
	// +kubebuilder:default=false
	PruneClusterResources bool `json:"pruneClusterResources,omitempty"`

	// PruneBlacklist can be used to specify a list of resources that are exempt
	// from pruning.
	// +optional
	PruneBlacklist []string `json:"pruneBlacklist,omitempty"`

	// RepositoryPath defines the relative path inside the Repository where the
	// configuration for this Waybill is stored. Accepted values are absolute
	// or relative paths (relative to the root of the repository), such as:
	// 'foo', '/foo', 'foo/bar', '/foo/bar' etc., as well as an empty string.
	// If not specified, it will default to the name of the namespace where the
	// Waybill is created.
	// +optional
	// +kubebuilder:validation:Pattern=^(\/?[a-zA-Z0-9.\_\-]+(\/[a-zA-Z0-9.\_\-]+)*\/?)?$
	RepositoryPath string `json:"repositoryPath"`

	// RunInterval determines how often this Waybill is applied in seconds.
	// +optional
	// +kubebuilder:default=3600
	RunInterval int `json:"runInterval,omitempty"`

	// ServerSideApply determines whether the server-side apply flag is enabled
	// for this Waybill.
	// +optional
	// +kubebuilder:default=false
	ServerSideApply bool `json:"serverSideApply,omitempty"`

	// StrongboxKeyringSecretRef references a Secret that contains an item named
	// '.strongbox_keyring' with any strongbox keys required to decrypt the
	// files before applying. See the strongbox documentation for the format of
	// the keyring data.
	// +optional
	StrongboxKeyringSecretRef *ObjectReference `json:"strongboxKeyringSecretRef,omitempty"`
}

// WaybillStatus defines the observed state of Waybill
type WaybillStatus struct {
	// LastRun contains the last apply run's information.
	// +nullable
	// +optional
	LastRun *WaybillStatusRun `json:"lastRun,omitempty"`
}

// WaybillStatusRun contains information about an apply run of a Waybill
// resource.
type WaybillStatusRun struct {
	// Command is the command used during the apply run.
	Command string `json:"command"`

	// Commit is the git commit hash on which this apply run operated.
	Commit string `json:"commit"`

	// ErrorMessage describes any errors that occured during the apply run.
	ErrorMessage string `json:"errorMessage"`

	// Finished is the time that the apply run finished applying this Waybill.
	Finished metav1.Time `json:"finished"`

	// Output is the stdout of the Command.
	Output string `json:"output"`

	// Started is the time that the apply run started applying this Waybill.
	Started metav1.Time `json:"started"`

	// Success denotes whether the apply run was successful or not.
	Success bool `json:"success"`

	// Type is a short description of the kind of apply run that was attempted.
	// +kubebuilder:default="unknown"
	Type string `json:"type"`
}

// +kubebuilder:object:root=true

// Waybill is the Schema for the Waybills API of kube-applier. A Waybill is
// defined as a namespace associated with a path in a remote git repository
// where kubernetes configuration is stored.
// +kubebuilder:resource:shortName=wb;wbs
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Success",type=boolean,JSONPath=`.status.lastRun.success`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.lastRun.type`
// +kubebuilder:printcolumn:name="Commit",type=string,JSONPath=`.status.lastRun.commit`
// +kubebuilder:printcolumn:name="Last Applied",type=date,JSONPath=`.status.lastRun.finished`
// +kubebuilder:printcolumn:name="Auto Apply",type=boolean,JSONPath=`.spec.autoApply`,priority=10
// +kubebuilder:printcolumn:name="Dry Run",type=boolean,JSONPath=`.spec.dryRun`,priority=10
// +kubebuilder:printcolumn:name="Prune",type=boolean,JSONPath=`.spec.prune`,priority=10
// +kubebuilder:printcolumn:name="Run Interval",type=number,JSONPath=`.spec.runInterval`,priority=10
// +kubebuilder:printcolumn:name="Repository Path",type=string,JSONPath=`.spec.repositoryPath`,priority=20
type Waybill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default={autoApply:true}
	// +optional
	Spec   WaybillSpec   `json:"spec,omitempty"`
	Status WaybillStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WaybillList contains a list of Waybill
type WaybillList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Waybill `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Waybill{}, &WaybillList{})
}
