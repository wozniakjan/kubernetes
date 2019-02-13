package deletion

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
)

// NamespaceConditionUpdater interface that translates namespace deleter errors
// into namespace status conditions
type NamespaceConditionUpdater interface {
	ProcessDiscoverResourcesErr(e error)
	ProcessGroupVersionErr(e error)
	ProcessDeleteContentErr(e error)
	Update(*v1.Namespace) bool
}

type namespaceConditionUpdater struct {
	newConditions       []v1.NamespaceCondition
	deleteContentErrors []error
}

var _ NamespaceConditionUpdater = &namespaceConditionUpdater{}

var (
	// conditionTypes Namespace condition types that are maintained by namespace_deleter controller
	conditionTypes = []v1.NamespaceConditionType{
		v1.NamespaceDeletionDiscoveryFailure,
		v1.NamespaceDeletionGVParsingFailure,
		v1.NamespaceDeletionContentFailure,
	}
	okMessages = map[v1.NamespaceConditionType]string{
		v1.NamespaceDeletionDiscoveryFailure: "All resources successfully discovered",
		v1.NamespaceDeletionGVParsingFailure: "All legacy kube types successfully parsed",
		v1.NamespaceDeletionContentFailure:   "All content successfully deleted",
	}
	okReasons = map[v1.NamespaceConditionType]string{
		v1.NamespaceDeletionDiscoveryFailure: "ResourcesDiscovered",
		v1.NamespaceDeletionGVParsingFailure: "ParsedGroupVersions",
		v1.NamespaceDeletionContentFailure:   "ContentDeleted",
	}
)

// ProcessGroupVersionErr creates error condition if parsing GroupVersion of resources fails
func (u *namespaceConditionUpdater) ProcessGroupVersionErr(err error) {
	d := v1.NamespaceCondition{
		Type:               v1.NamespaceDeletionGVParsingFailure,
		Status:             v1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		LastProbeTime:      metav1.Now(),
		Reason:             "GroupVersionParsingFailed",
		Message:            err.Error(),
	}
	u.newConditions = append(u.newConditions, d)
}

// ProcessDiscoverResourcesErr creates error condition from ErrGroupDiscoveryFailed
func (u *namespaceConditionUpdater) ProcessDiscoverResourcesErr(err error) {
	var msg string
	if derr, ok := err.(*discovery.ErrGroupDiscoveryFailed); ok {
		msg = fmt.Sprintf("Discovery failed for some groups, %d failing: %v", len(derr.Groups), err)
	} else {
		msg = err.Error()
	}
	d := v1.NamespaceCondition{
		Type:               v1.NamespaceDeletionDiscoveryFailure,
		Status:             v1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		LastProbeTime:      metav1.Now(),
		Reason:             "DiscoveryFailed",
		Message:            msg,
	}
	u.newConditions = append(u.newConditions, d)

}

// ProcessDeleteContentErr creates error condition from multiple delete content errors
func (u *namespaceConditionUpdater) ProcessDeleteContentErr(err error) {
	u.deleteContentErrors = append(u.deleteContentErrors, err)
}

// Update compiles processed errors from namespace deletion into status conditions
func (u *namespaceConditionUpdater) Update(ns *v1.Namespace) bool {
	if c := getCondition(u.newConditions, v1.NamespaceDeletionContentFailure); c == nil {
		if c := makeDeleteContentCondition(u.deleteContentErrors); c != nil {
			u.newConditions = append(u.newConditions, *c)
		}
	}
	return updateConditions(&ns.Status, u.newConditions)
}

func makeDeleteContentCondition(err []error) *v1.NamespaceCondition {
	if len(err) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(err))
	for _, e := range err {
		msgs = append(msgs, e.Error())
	}
	sort.Strings(msgs)
	return &v1.NamespaceCondition{
		Type:               v1.NamespaceDeletionContentFailure,
		Status:             v1.ConditionTrue,
		LastProbeTime:      metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             "ContentDeletionFailed",
		Message:            fmt.Sprintf("Failed to delete all resource types, %d remaining: %v", len(err), strings.Join(msgs, ", ")),
	}
}

func updateConditions(status *v1.NamespaceStatus, newConditions []v1.NamespaceCondition) (hasChanged bool) {
	for _, conditionType := range conditionTypes {
		newCondition := getCondition(newConditions, conditionType)
		oldCondition := getCondition(status.Conditions, conditionType)
		if newCondition == nil && oldCondition == nil {
			// both are nil, no update necessary
			continue
		}
		if oldCondition == nil {
			// only new condition of this type exists, add to the list
			status.Conditions = append(status.Conditions, *newCondition)
			hasChanged = true
		} else if newCondition == nil {
			// only old condition of this type exists, set status to false
			if oldCondition.Status != v1.ConditionFalse {
				oldCondition.Status = v1.ConditionFalse
				oldCondition.Message = okMessages[conditionType]
				oldCondition.Reason = okReasons[conditionType]
				oldCondition.LastTransitionTime = metav1.Now()
				hasChanged = true
			}
		} else if oldCondition.Message != newCondition.Message {
			// old condition needs to be updated
			if oldCondition.Status != newCondition.Status {
				oldCondition.LastTransitionTime = metav1.Now()
			}
			oldCondition.Type = newCondition.Type
			oldCondition.Status = newCondition.Status
			oldCondition.LastProbeTime = newCondition.LastProbeTime
			oldCondition.Reason = newCondition.Reason
			oldCondition.Message = newCondition.Message
			hasChanged = true
		}
	}
	return
}

func getCondition(conditions []v1.NamespaceCondition, conditionType v1.NamespaceConditionType) *v1.NamespaceCondition {
	for i, _ := range conditions {
		if conditions[i].Type == conditionType {
			return &(conditions[i])
		}
	}
	return nil
}
