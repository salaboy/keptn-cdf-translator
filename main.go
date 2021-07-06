package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/cdfoundation/sig-events/cde/sdk/go/pkg/cdf/events"
	cdeextensions "github.com/cdfoundation/sig-events/cde/sdk/go/pkg/cdf/events/extensions"
	"github.com/cloudevents/sdk-go/v2/binding"
	cehttp "github.com/cloudevents/sdk-go/v2/protocol/http"
	"github.com/gorilla/mux"
	"github.com/salaboy/keptn-cdf-translator/pkg/handlers"
)

var (
	KEPTN_ENDPOINT = os.Getenv("KEPTN_ENDPOINT")
	KEPTN_API_TOKEN = os.Getenv("KEPTN_API_TOKEN")

	// To get the API Token you need to run: KEPTN_API_TOKEN=$(kubectl get secret keptn-api-token -n keptn -ojsonpath={.data.keptn-api-token} | base64 --decode)
	registry = handlers.CDEventHandlerRegistry{}
	artifactExtension = cdeextensions.ArtifactExtension{}
	serviceExtension = cdeextensions.ServiceExtension{}
)

func main() {
	if  KEPTN_ENDPOINT == "" {
		KEPTN_ENDPOINT = "http://localhost:8080/api/"
	}

	keptnEndpoint, err  := url.Parse(KEPTN_ENDPOINT)
	if err != nil {
		log.Fatal(err)
	}
	registry.Sink = *keptnEndpoint
	registry.KeptnApiToken = KEPTN_API_TOKEN

	artifactPackagedtHandler := handlers.ArtifactPackagedEventHandler{}
	registry.AddHandler(events.ArtifactPackagedEventV1.String(), &artifactPackagedtHandler)
	artifactPublishedHandler := handlers.ArtifactPublishedEventHandler{}
	registry.AddHandler(events.ArtifactPublishedEventV1.String(), &artifactPublishedHandler)
	serviceHandler := handlers.ServiceDeployedEventHandler{}
	registry.AddHandler(events.ServiceDeployedEventV1.String(), &serviceHandler)

	log.Printf("Configuration > Keptn Endpoint: " + KEPTN_ENDPOINT + " and KEPTN API TOKEN: " + KEPTN_API_TOKEN)
	log.Printf("CDF to Keptn Translator Started in port 8081!")

	r := mux.NewRouter()
	r.HandleFunc("/events", EventsHandler).Methods("POST")

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func EventsHandler(writer http.ResponseWriter, request *http.Request) {
	ctx := context.Background()
	message := cehttp.NewMessageFromHttpRequest(request)
	event, _ := binding.ToEvent(ctx, message,
		artifactExtension.ReadTransformer(), artifactExtension.WriteTransformer(),
		serviceExtension.ReadTransformer(), serviceExtension.WriteTransformer())
	log.Printf("Got an Event: %s", event.Type())
	registry.HandleEvent(event)
}
