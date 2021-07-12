package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	cdeextensions "github.com/cdfoundation/sig-events/cde/sdk/go/pkg/cdf/events/extensions"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/event"
	apimodels "github.com/keptn/go-utils/pkg/api/models"
	apiutils "github.com/keptn/go-utils/pkg/api/utils"
	keptnv2 "github.com/keptn/go-utils/pkg/lib/v0_2_0"
)

type CDEventHandlerRegistry struct {
	Sink          url.URL
	KeptnApiToken string
	handlers      map[string]CDEventHandler
}

func (r *CDEventHandlerRegistry) AddHandler(eventType string, handler CDEventHandler) {
	if r.handlers == nil {
		r.handlers = make(map[string]CDEventHandler)
	}
	r.handlers[eventType] = handler
}

func (r *CDEventHandlerRegistry) HandleEvent(e *event.Event) {
	if r.handlers[e.Type()] == nil {
		log.Println("no Handlers for type: " + e.Type())
	} else {
		keptnEventBuilders := r.handlers[e.Type()].HandleCDEvent(e)
		keptnContext := ""
		keptnTriggerId := ""
		for _, keptnEventBuilder := range keptnEventBuilders {
			// For the first event, context and trigger ID will empty
			// If so, check if the builder includes them, if so don't override them
			if keptnContext == "" {
				keptnContext = keptnEventBuilder.Shkeptncontext
			}
			if keptnTriggerId == "" {
				keptnTriggerId = keptnEventBuilder.Triggeredid
			}
			keptnEventBuilderWithContext := keptnEventBuilder.WithKeptnContext(keptnContext).WithTriggeredID(keptnTriggerId)
			keptnEvent, err := keptnEventBuilderWithContext.Build()
			if err != nil {
				// if something goes wrong we won't handle this
				log.Printf("failed to build cloud event. %s\n", err.Error())
				return
			}
			// Store the context in case that we send more than one event
			eventContext := handleKeptnEvent(keptnEvent, r.Sink, r.KeptnApiToken)
			keptnContext = *eventContext.KeptnContext
			keptnTriggerId = *eventContext.KeptnContext
		}
	}
}

func handleKeptnEvent(keptnEvent apimodels.KeptnContextExtendedCE, sink url.URL, token string) *apimodels.EventContext {
	log.Printf("Emitting an Event: %T to Sink: %s\n", keptnEvent, sink)
	// Set a target.

	apiHandler := apiutils.NewAuthenticatedAPIHandler(sink.String(), token, "x-token", nil, sink.Scheme)
	log.Printf("apiHandler: %q \n", apiHandler)

	eventContext, err := apiHandler.SendEvent(keptnEvent)
	if err != nil {
		log.Printf("sending keptn event was unsuccessful. %s", *err.Message)
		return nil
	}
	if eventContext != nil {
		log.Println("Got Keptn context: %s", *eventContext.KeptnContext)
		return eventContext
	}
	return nil
}

type CDEventHandler interface {
	HandleCDEvent(e *cloudevents.Event) []*keptnv2.KeptnEventBuilder
}

type ArtifactPackagedEventHandler struct{}

// HandleCDEvent for ArtifactPackagedEventHandler sends a DeploymentTriggered event
func (n *ArtifactPackagedEventHandler) HandleCDEvent(e *event.Event) []*keptnv2.KeptnEventBuilder {
	artifactExtension := cdeextensions.ArtifactExtension{}
	e.ExtensionAs(cdeextensions.ArtifactIdExtension, &artifactExtension.ArtifactId)
	e.ExtensionAs(cdeextensions.ArtifactNameExtension, &artifactExtension.ArtifactName)
	e.ExtensionAs(cdeextensions.ArtifactVersionExtension, &artifactExtension.ArtifactVersion)

	eventData := keptnv2.EventData{
		Project: "cde",
		Stage:   "production",
		Service: artifactExtension.ArtifactName,
		Message: "deployment handled by Tekton",
	}

	deploymentTriggeredData := keptnv2.DeploymentTriggeredEventData{
		EventData: eventData,
		ConfigurationChange: keptnv2.ConfigurationChange{
			Values: map[string]interface{}{
				"image": artifactExtension.ArtifactId,
			},
		},
	}
	deploymentTriggered := keptnv2.KeptnEvent(
		keptnv2.GetTriggeredEventType("production.delivery"),
		"keptn-cdf-translator",
		deploymentTriggeredData)

	return []*keptnv2.KeptnEventBuilder{deploymentTriggered}
}

type ServiceDeployedEventHandler struct {}

func (n *ServiceDeployedEventHandler) HandleCDEvent(e *event.Event) []*keptnv2.KeptnEventBuilder {
	serviceExtension := cdeextensions.ServiceExtension{}
	e.ExtensionAs(cdeextensions.ServiceEnvIdExtension, &serviceExtension.ServiceEnvId)
	e.ExtensionAs(cdeextensions.ServiceNameExtension, &serviceExtension.ServiceName)
	e.ExtensionAs(cdeextensions.ServiceVersionExtension, &serviceExtension.ServiceVersion)
	targetURL := fmt.Sprintf("http://%s-127.0.0.1.nip.io", serviceExtension.ServiceName)

	// Service name is mandatory
	if serviceExtension.ServiceName == "" {
		log.Printf("no service name found in event %s, using \"poc\"\n", *e)
	}

	// Build the keptn event data
	// Hardcode passed result for now. We could read it from the PipelineRun
	deploymentEvent := keptnv2.DeploymentFinishedEventData{
		EventData: keptnv2.EventData{
			Project: "cde",
			Stage:   "production",
			Service: serviceExtension.ServiceName,
			Status: keptnv2.StatusSucceeded,
			Result: keptnv2.ResultPass,
		},
		Deployment: keptnv2.DeploymentFinishedData{
			DeploymentStrategy: "direct",
			DeploymentURIsLocal: []string{ targetURL },
			DeploymentURIsPublic: []string{ targetURL },
			DeploymentNames: []string{ serviceExtension.ServiceName },
			GitCommit: "main",
		},
	}

	// Build the keptn event context
	keptnEventContext := keptnv2.KeptnEvent(
		keptnv2.GetFinishedEventType("deployment"),
		"keptn-cdf-translator",
		deploymentEvent)

	// Extract trigger id and context from the data
	type tektonResult struct {
		Name  string  `json:"name"`
		Value string  `json:"value"`
	}
	type tektonPipelineRun struct {
		Status struct {
			PipelineResults []tektonResult `json:"pipelineResults"`
		} `json:"status"`
	}
	var data map[string]interface{}
	json.Unmarshal(e.Data(), &data)
	if pr, ok := data["pipelinerun"]; ok {
		var tpr tektonPipelineRun
		json.Unmarshal([]byte(pr.(string)), &tpr)
		log.Printf("tpr status: %s\n", tpr.Status)
		for _, v := range tpr.Status.PipelineResults {
			switch v.Name {
			case "sh.keptn.context":
				keptnEventContext.WithKeptnContext(v.Value)
				log.Printf("received context: %s\n", v.Value)
			case "sh.keptn.trigger.id":
				keptnEventContext.WithTriggeredID(v.Value)
				log.Printf("received trigger id: %s\n", v.Value)
			}
		}
	}

	log.Printf("Deployment Event Context %s\n", keptnEventContext)
	return []*keptnv2.KeptnEventBuilder{keptnEventContext}
}

type ArtifactPublishedEventHandler struct{}

// HandleCDEvent for ArtifactPublishedEventHandler sends a DeploymentStarted event
func (n *ArtifactPublishedEventHandler) HandleCDEvent(e *event.Event) []*keptnv2.KeptnEventBuilder {
	artifactExtension := cdeextensions.ArtifactExtension{}
	e.ExtensionAs(cdeextensions.ArtifactIdExtension, &artifactExtension.ArtifactId)
	e.ExtensionAs(cdeextensions.ArtifactNameExtension, &artifactExtension.ArtifactName)
	e.ExtensionAs(cdeextensions.ArtifactVersionExtension, &artifactExtension.ArtifactVersion)

	eventData := keptnv2.EventData{
		Project: "cde",
		Stage:   "production",
		Service: artifactExtension.ArtifactName,
		Message: "deployment handled by Tekton",
		Status:  keptnv2.StatusUnknown,
	}

	deploymentStartedData := keptnv2.DeploymentStartedEventData{
		EventData: eventData,
	}
	deploymentStarted := keptnv2.KeptnEvent(
		keptnv2.GetStartedEventType("deployment"),
		"keptn-cdf-translator",
		deploymentStartedData)

	var data map[string]interface{}
	json.Unmarshal(e.Data(), &data)
	if ctx, ok := data["shkeptncontext"]; ok {
		deploymentStarted = deploymentStarted.WithKeptnContext(ctx.(string))
		log.Printf("Found Keptn context %s\n", ctx.(string))
	}
	if triggerid, ok := data["triggerid"]; ok {
		deploymentStarted = deploymentStarted.WithTriggeredID(triggerid.(string))
		log.Printf("Found Keptn trigger ID %s\n", triggerid.(string))
	}

	log.Printf("Deployment Event Context %s\n", deploymentStarted)

	return []*keptnv2.KeptnEventBuilder{deploymentStarted}
}