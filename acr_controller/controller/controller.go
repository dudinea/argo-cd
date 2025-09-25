package application_change_revision_controller

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	appclient "github.com/argoproj/argo-cd/v3/acr_controller/application"
	"github.com/argoproj/argo-cd/v3/acr_controller/service"
	appclientset "github.com/argoproj/argo-cd/v3/pkg/client/clientset/versioned"

	appv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

var watchAPIBufferSize = 1000

type ACRController interface {
	Run(ctx context.Context)
}

type applicationChangeRevisionController struct {
	appBroadcaster Broadcaster
	acrService     service.ACRService
	useAnnotations bool
}

func NewApplicationChangeRevisionController(appInformer cache.SharedIndexInformer, applicationServiceClient appclient.ApplicationClient, applicationClientset appclientset.Interface, useAnnotations bool) ACRController {
	appBroadcaster := NewBroadcaster()
	_, err := appInformer.AddEventHandler(appBroadcaster)
	if err != nil {
		log.Error(err)
	}
	return &applicationChangeRevisionController{
		appBroadcaster: appBroadcaster,
		acrService:     service.NewACRService(applicationClientset, applicationServiceClient),
		useAnnotations: useAnnotations,
	}
}

func (c *applicationChangeRevisionController) Run(ctx context.Context) {
	var logCtx log.FieldLogger = log.StandardLogger()

	calculateIfPermitted := func(ctx context.Context, a appv1.Application, eventType watch.EventType) error { //nolint:golint,unparam
		if eventType == watch.Bookmark || eventType == watch.Deleted {
			return nil // ignore this event
		}

		return c.acrService.ChangeRevision(ctx, &a, c.useAnnotations)
	}

	// TODO: move to abstraction
	eventsChannel := make(chan *appv1.ApplicationWatchEvent, watchAPIBufferSize)
	unsubscribe := c.appBroadcaster.Subscribe(eventsChannel)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-eventsChannel:
			// logCtx.Infof("channel size is %d", len(eventsChannel))

			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err := calculateIfPermitted(ctx, event.Application, event.Type)
			if err != nil {
				logCtx.WithError(err).Error("failed to calculate change revision")
			}
			cancel()
		}
	}
}
