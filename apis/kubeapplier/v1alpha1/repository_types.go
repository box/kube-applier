package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RepositorySpec defines the desired state of Repository
type RepositorySpec struct {
	// RemoteUrl is the URL where the git remote is hosted.
	RemoteUrl string `json:"remoteUrl"`

	// SecretName is the name of the Secret resource containing the SSH key
	// required for authenticating with the remote.
	// +optional
	SecretName string `json:"secretName,omitempty"`
}

// RepositoryStatus defines the observed state of Repository
type RepositoryStatus struct {
	// HeadCommit is the current git commit hash of the Repository's HEAD.
	// +optional
	HeadCommit string `json:"headCommit,omitempty"`

	// SyncedAt is the time that the Repository was last synced from the remote.
	// +optional
	SyncedAt metav1.Time `json:"finished,omitempty"`
}

// +kubebuilder:object:root=true

// Repository is the Schema for the repositories API
// +kubebuilder:resource:shortName=repo;repos
// +kubebuilder:subresource:status
type Repository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RepositorySpec   `json:"spec,omitempty"`
	Status RepositoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RepositoryList contains a list of Repository
type RepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Repository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Repository{}, &RepositoryList{})
}
