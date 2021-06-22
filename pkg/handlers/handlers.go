package handlers

import (
	"encoding/json"
	"fmt"
	cdeextensions "github.com/cdfoundation/sig-events/cde/sdk/go/pkg/cdf/events/extensions"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/google/uuid"
	apimodels "github.com/keptn/go-utils/pkg/api/models"
	apiutils "github.com/keptn/go-utils/pkg/api/utils"
	keptnv2 "github.com/keptn/go-utils/pkg/lib/v0_2_0"
	"net/url"
	"time"
)

type CDEventHandlerRegistry struct {
	Sink          string
	KeptnApiToken string
	handlers      map[string]CDEventHandler
}

func (r *CDEventHandlerRegistry) AddHandler(eventType string, handler CDEventHandler) {
	handler.SetSink(r.Sink)
	handler.SetKeptnApiToken(r.KeptnApiToken)
	if r.handlers == nil {
		r.handlers = make(map[string]CDEventHandler)
	}
	r.handlers[eventType] = handler
}

func (r *CDEventHandlerRegistry) HandleEvent(e *event.Event) {
	if r.handlers[e.Type()] == nil {
		fmt.Println("No Handlers for type: " + e.Type())
	} else {
		r.handlers[e.Type()].HandleCDEvent(e)
	}
}

type CDEventHandler interface {
	HandleCDEvent(e *event.Event)
	SetSink(sink string)
	SetKeptnApiToken(keptnApiToken string)
}

type GenericHandler func(e *event.Event)

func (h GenericHandler) HandleCDEvent(e *event.Event) {
	h(e)
}

type ArtifactPackagedEventHandler struct {
	Sink         string
	KetnApiToken string
}

func (n *ArtifactPackagedEventHandler) SetSink(sink string) {
	n.Sink = sink
}

func (n *ArtifactPackagedEventHandler) SetKeptnApiToken(apiToken string) {
	n.KetnApiToken = apiToken
}

func (n *ArtifactPackagedEventHandler) HandleCDEvent(e *event.Event) {
	artifactExtension := cdeextensions.ArtifactExtension{}
	e.ExtensionAs(cdeextensions.ArtifactIdExtension, &artifactExtension.ArtifactId)
	e.ExtensionAs(cdeextensions.ArtifactNameExtension, &artifactExtension.ArtifactName)
	e.ExtensionAs(cdeextensions.ArtifactVersionExtension, &artifactExtension.ArtifactVersion)

	deploymentEvent := keptnv2.DeploymentTriggeredEventData{
		EventData: keptnv2.EventData{
			Project: "fmtok8s",
			Stage:   "production",
			Service: artifactExtension.ArtifactName,
		},
		ConfigurationChange: keptnv2.ConfigurationChange{
			Values: map[string]interface{}{
				"image": "docker.io/salaboy/" + artifactExtension.ArtifactName + ":" + artifactExtension.ArtifactVersion,
			},
		},
	}

	newEvent := cloudevents.NewEvent()
	newUUID, _ := uuid.NewUUID()
	newEvent.SetID(newUUID.String())
	newEvent.SetTime(time.Now())
	newEvent.SetSource("keptn-cdf-translator")
	newEvent.SetDataContentType(cloudevents.ApplicationJSON)
	newEvent.SetType("sh.keptn.event.production.delivery.triggered")
	newEvent.SetData(cloudevents.ApplicationJSON, deploymentEvent)

	fmt.Printf("Emitting an Event: %s to Sink: %s", newEvent, n.Sink)
	// Set a target.

	eventByte, err := newEvent.MarshalJSON()
	if err != nil {
		fmt.Errorf("Failed to marshal cloud event. %s", err.Error())
	}
	fmt.Println("> Cloud Event in JSON: " + string(eventByte))

	apiEvent := apimodels.KeptnContextExtendedCE{}
	err = json.Unmarshal(eventByte, &apiEvent)
	if err != nil {
		fmt.Errorf("Failed to map cloud event to API event model. %v", err)
	}

	endPoint, _ := url.Parse(n.Sink)

	apiHandler := apiutils.NewAuthenticatedAPIHandler(endPoint.String(), n.KetnApiToken, "x-token", nil, endPoint.Scheme)

	eventContext, err2 := apiHandler.SendEvent(apiEvent)
	if err2 != nil {
		fmt.Errorf("trigger delivery was unsuccessful. %s", *err2.Message)
		return
	}
	if eventContext != nil {
		fmt.Println("I should store this context somewhere: " + *eventContext.KeptnContext)
	}
}
