package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	argoclient "github.com/argoproj/argo-cd/v3/acr_controller/application"
	appclient "github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	application "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	appclientset "github.com/argoproj/argo-cd/v3/pkg/client/clientset/versioned"
)

const (
	CHANGE_REVISION_ANN  = "mrp-controller.argoproj.io/change-revision"
	CHANGE_REVISIONS_ANN = "mrp-controller.argoproj.io/change-revisions"
	GIT_REVISION_ANN     = "mrp-controller.argoproj.io/git-revision"
	GIT_REVISIONS_ANN    = "mrp-controller.argoproj.io/git-revisions"
)

type ACRService interface {
	ChangeRevision(ctx context.Context, application *application.Application, useAnnotations bool) error
}

type acrService struct {
	applicationClientset     appclientset.Interface
	applicationServiceClient argoclient.ApplicationClient
	lock                     sync.Mutex
	logger                   *log.Logger
}

func NewACRService(applicationClientset appclientset.Interface, applicationServiceClient argoclient.ApplicationClient) ACRService {
	return &acrService{
		applicationClientset:     applicationClientset,
		applicationServiceClient: applicationServiceClient,
		logger:                   log.New(),
	}
}

func getChangeRevisionFromRevisions(revisions []string) string {
	if len(revisions) > 0 {
		return revisions[0]
	}
	return ""
}

func getChangeRevision(app *application.Application) string {
	if app.Status.OperationState != nil && app.Status.OperationState.Operation.Sync != nil {
		changeRevision := app.Status.OperationState.Operation.Sync.ChangeRevision
		if changeRevision != "" {
			return changeRevision
		}
		if changeRevision = getChangeRevisionFromRevisions(app.Status.OperationState.Operation.Sync.ChangeRevisions); changeRevision != "" {
			return changeRevision
		}
	}
	return ""
}

func (c *acrService) ChangeRevision(ctx context.Context, a *application.Application, useAnnotations bool) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	app, err := c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Get(ctx, a.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if app.Operation == nil || app.Operation.Sync == nil {
		return nil
	}

	if getChangeRevision(app) != "" {
		c.logger.Infof("Change revision already calculated for application %s", app.Name)
		return nil
	}

	currentRevision, previousRevision := c.getRevisions(ctx, a)
	revision, err := c.calculateRevision(ctx, app, currentRevision, previousRevision)
	if err != nil {
		return err
	}

	var revisions []string
	if revision == nil || *revision == "" {
		c.logger.Infof("Revision for application %s is empty", app.Name)
	} else {
		c.logger.Infof("Change revision for application %s is %s", app.Name, *revision)
		revisions = []string{*revision}
	}

	app, err = c.applicationClientset.ArgoprojV1alpha1().Applications(app.Namespace).Get(ctx, app.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	patchMap := make(map[string]any, 2)

	if len(revisions) > 0 {
		if app.Status.OperationState != nil && app.Status.OperationState.Operation.Sync != nil {
			c.logger.Infof("Patch operation status for application %s", app.Name)
			patchMap = c.patchOperationSyncResultWithChangeRevision(ctx, app, revisions)
		} else {
			c.logger.Infof("Patch operation for application %s", app.Name)
			patchMap = c.patchOperationWithChangeRevision(ctx, app, revisions)
		}
	}
	if useAnnotations {
		err = c.addAnnotationPatch(patchMap, app, *revision, revisions, currentRevision, []string{currentRevision})
		if err != nil {
			return err
		}
	}
	if len(patchMap) > 0 {
		c.logger.Infof("patching resource: %v", patchMap)
		patch, err := json.Marshal(patchMap)
		if err != nil {
			return err
		}
		_, err = c.applicationClientset.ArgoprojV1alpha1().Applications(a.Namespace).Patch(ctx, a.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	} else {
		c.logger.Infof("no patch needed")
		return nil
	}
}

func addPatchIfNeeded(annotations map[string]string, currentAnnotations map[string]string, key string, val string) {
	currentVal, ok := currentAnnotations[key]
	if !ok || currentVal != val {
		annotations[key] = val
	}
}

func (c *acrService) addAnnotationPatch(m map[string]any,
	a *application.Application,
	changeRevision string,
	changeRevisions []string,
	gitRevision string,
	gitRevisions []string) error {
	c.logger.Infof("annotating application '%s', changeRevision=%s, changeRevisions=%v, gitRevision=%s, gitRevisions=%v", a.Name, changeRevision, changeRevisions, gitRevision, gitRevisions)
	annotations := map[string]string{}
	currentAnnotations := a.Annotations

	changeRevisionsJson, err := json.Marshal(changeRevisions)
	if err != nil {
		return fmt.Errorf("Failed to marshall changeRevisions %v: %v", changeRevisions, err)
	}
	gitRevisionsJson, err := json.Marshal(gitRevisions)
	if err != nil {
		return fmt.Errorf("Failed to marshall gitRevisions %v: %v", gitRevisions, err)
	}

	addPatchIfNeeded(annotations, currentAnnotations, CHANGE_REVISION_ANN, changeRevision)
	addPatchIfNeeded(annotations, currentAnnotations, CHANGE_REVISIONS_ANN, string(changeRevisionsJson))
	addPatchIfNeeded(annotations, currentAnnotations, GIT_REVISION_ANN, gitRevision)
	addPatchIfNeeded(annotations, currentAnnotations, GIT_REVISIONS_ANN, string(gitRevisionsJson))

	if len(annotations) == 0 {
		c.logger.Info("no need to add annotations")
	}
	c.logger.Infof("added annotations to application %s patch: %v", a.Name, annotations)
	m["metadata"] = map[string]any{"annotations": annotations}
	return nil
}

func (c *acrService) calculateRevision(ctx context.Context, a *application.Application, currentRevision string, previousRevision string) (*string, error) {
	c.logger.Infof("Calculate revision for application '%s', current revision '%s', previous revision '%s'", a.Name, currentRevision, previousRevision)
	changeRevisionResult, err := c.applicationServiceClient.GetChangeRevision(ctx, &appclient.ChangeRevisionRequest{
		AppName:          ptr.To(a.GetName()),
		Namespace:        ptr.To(a.GetNamespace()),
		CurrentRevision:  ptr.To(currentRevision),
		PreviousRevision: ptr.To(previousRevision),
	})
	if err != nil {
		return nil, err
	}
	return changeRevisionResult.Revision, nil
}

func (c *acrService) patchOperationWithChangeRevision(ctx context.Context, a *application.Application, revisions []string) map[string]any {
	if len(revisions) == 1 {
		return map[string]any{
			"operation": map[string]any{
				"sync": map[string]any{
					"changeRevision": revisions[0],
				},
			},
		}
	}
	return map[string]any{
		"operation": map[string]any{
			"sync": map[string]any{
				"changeRevisions": revisions,
			},
		},
	}
}

func (c *acrService) patchOperationSyncResultWithChangeRevision(ctx context.Context, a *application.Application, revisions []string) map[string]any {
	if len(revisions) == 1 {
		return map[string]any{
			"status": map[string]any{
				"operationState": map[string]any{
					"operation": map[string]any{
						"sync": map[string]any{
							"changeRevision": revisions[0],
						},
					},
				},
			},
		}
	}
	return map[string]any{
		"status": map[string]any{
			"operationState": map[string]any{
				"operation": map[string]any{
					"sync": map[string]any{
						"changeRevisions": revisions,
					},
				},
			},
		},
	}
}

func getCurrentRevisionFromOperation(a *application.Application) string {
	if a.Operation != nil && a.Operation.Sync != nil {
		return a.Operation.Sync.Revision
	}
	return ""
}

func (c *acrService) getRevisions(_ context.Context, a *application.Application) (string, string) {
	if len(a.Status.History) == 0 {
		// it is first sync operation, and we have only current revision
		return getCurrentRevisionFromOperation(a), ""
	}

	// in case if sync is already done, we need to use revision from sync result and previous revision from history
	if a.Status.Sync.Status == "Synced" && a.Status.OperationState != nil && a.Status.OperationState.SyncResult != nil {
		currentRevision := a.Status.OperationState.SyncResult.Revision
		// in case if we have only one history record, we need to return empty previous revision, because it is first sync result
		if len(a.Status.History) == 1 {
			return currentRevision, ""
		}
		return currentRevision, a.Status.History[len(a.Status.History)-2].Revision
	}

	// in case if sync is in progress, we need to use revision from operation and revision from latest history record
	currentRevision := getCurrentRevisionFromOperation(a)
	previousRevision := a.Status.History[len(a.Status.History)-1].Revision
	return currentRevision, previousRevision
}
