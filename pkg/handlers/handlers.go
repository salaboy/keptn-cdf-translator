package handlers

import (
	"encoding/json"
	"fmt"
	cdeextensions "github.com/cdfoundation/sig-events/cde/sdk/go/pkg/cdf/events/extensions"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/event"
	apimodels "github.com/keptn/go-utils/pkg/api/models"
	apiutils "github.com/keptn/go-utils/pkg/api/utils"
	keptnv2 "github.com/keptn/go-utils/pkg/lib/v0_2_0"
	"net/url"
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
		fmt.Println("no Handlers for type: " + e.Type())
	} else {
		keptnEvent, err := r.handlers[e.Type()].HandleCDEvent(e)
		if err != nil {
			// if something goes wrong we won't handle this
			fmt.Printf("failed to build cloud event. %s", err.Error())
			return
		}
		handleKeptnEvnt(keptnEvent, r.Sink, r.KeptnApiToken)
	}
}

func handleKeptnEvnt(keptnEvent apimodels.KeptnContextExtendedCE, sink url.URL, token string) {
	fmt.Printf("Emitting an Event: %T to Sink: %s", keptnEvent, sink)
	// Set a target.

	apiHandler := apiutils.NewAuthenticatedAPIHandler(sink.String(), token, "x-token", nil, sink.Scheme)
	fmt.Printf("apiHandler: %q \n", apiHandler)

	eventContext, err := apiHandler.SendEvent(keptnEvent)
	if err != nil {
		fmt.Errorf("sending keptn event was unsuccessful. %s", *err.Message)
		return
	}
	if eventContext != nil {
		fmt.Println("I should store this context somewhere: " + *eventContext.KeptnContext)
	}
}

type CDEventHandler interface {
	HandleCDEvent(e *cloudevents.Event) (apimodels.KeptnContextExtendedCE, error)
}

type ArtifactPackagedEventHandler struct{}

func (n *ArtifactPackagedEventHandler) HandleCDEvent(e *event.Event) (apimodels.KeptnContextExtendedCE, error) {
	artifactExtension := cdeextensions.ArtifactExtension{}
	e.ExtensionAs(cdeextensions.ArtifactIdExtension, &artifactExtension.ArtifactId)
	e.ExtensionAs(cdeextensions.ArtifactNameExtension, &artifactExtension.ArtifactName)
	e.ExtensionAs(cdeextensions.ArtifactVersionExtension, &artifactExtension.ArtifactVersion)

	deploymentEvent := keptnv2.DeploymentTriggeredEventData{
		EventData: keptnv2.EventData{
			Project: "cde",
			Stage:   "production",
			Service: artifactExtension.ArtifactName,
		},
		ConfigurationChange: keptnv2.ConfigurationChange{
			Values: map[string]interface{}{
				"image": artifactExtension.ArtifactId,
			},
		},
	}

	// newEvent is a KeptnContextExtendedCE
	return keptnv2.KeptnEvent(
		keptnv2.GetTriggeredEventType("production.delivery"),
		"keptn-cdf-translator",
		deploymentEvent).Build()
}

type ServiceDeployedEventHandler struct {}

func (n *ServiceDeployedEventHandler) HandleCDEvent(e *event.Event) (apimodels.KeptnContextExtendedCE, error) {
	serviceExtension := cdeextensions.ServiceExtension{}
	e.ExtensionAs(cdeextensions.ServiceEnvIdExtension, &serviceExtension.ServiceEnvId)
	e.ExtensionAs(cdeextensions.ServiceNameExtension, &serviceExtension.ServiceName)
	e.ExtensionAs(cdeextensions.ServiceVersionExtension, &serviceExtension.ServiceVersion)
	targetURL := fmt.Sprintf("http://localhost/%s/", serviceExtension.ServiceName)

	// Service name is mandatory
	if serviceExtension.ServiceName == "" {
		fmt.Printf("No service name found in event %s, using \"poc\"", *e)
	}

	// Build the keptn event data
	deploymentEvent := keptnv2.DeploymentFinishedEventData{
		EventData: keptnv2.EventData{
			Project: "cde",
			Stage:   "production",
			Service: serviceExtension.ServiceName,
		},
		Deployment: keptnv2.DeploymentFinishedData{
			DeploymentStrategy: "user_managed",
			DeploymentURIsLocal: []string{ targetURL },
			DeploymentURIsPublic: []string{ targetURL },
			DeploymentNames: []string{ serviceExtension.ServiceName },
			GitCommit: "main",
		},
	}

	// Build the keptn event context
	keptnEventContext := keptnv2.KeptnEvent(
		keptnv2.GetFinishedEventType("production.deployment"),
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
	if pr, ok := data["pipelineRun"]; ok {
		tpr := pr.(tektonPipelineRun)
		for _, v := range tpr.Status.PipelineResults {
			switch v.Name {
			case "sh.keptn.context":
				keptnEventContext = keptnEventContext.WithKeptnContext(v.Value)
			case "sh.keptn.trigger.id":
				keptnEventContext = keptnEventContext.WithTriggeredID(v.Value)
			}
		}
	}

	fmt.Printf("Deployment Event Context %T", keptnEventContext)

	return keptnEventContext.Build()
}
