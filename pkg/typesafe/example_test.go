package typesafe_test

import (
	"context"
	"fmt"
	"os"

	"github.com/atomicobject/rhizome/pkg/typesafe"
)

func ExampleClient_Choose() {
	client, err := typesafe.NewClient(os.Getenv("TYPESAFE_API_KEY"), typesafe.WithModel("jev-1.13.0"))
	if err != nil {
		panic(err)
	}
	state := struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}{"Retry policy", "Retry rate-limited requests after the specified delay."}
	result, err := client.Choose(context.Background(), state, typesafe.Choice{
		Instructions: "Which document category fits?",
		Criteria: map[string]any{
			"spec":      "Required system behavior",
			"reference": "Explanation of existing behavior",
			"other":     "Neither category fits",
		},
	})
	if err != nil {
		panic(err)
	}
	// Answer is a ChoiceAnswer at compile time, with no JSON parsing or assertion.
	fmt.Println(result.Answer.Choice, result.Answer.Confidence, result.Usage.InputTokens)
}

func ExampleClient_Evaluate() {
	client, err := typesafe.NewClient(os.Getenv("TYPESAFE_API_KEY"))
	if err != nil {
		panic(err)
	}
	response, err := client.Evaluate(context.Background(), typesafe.Request{
		State: map[string]string{"requirement": "Retry 429 responses", "evidence": "The client retries 429 with backoff."},
		Questions: map[string]typesafe.Question{
			"support": typesafe.Noul{Instructions: "Does the evidence support the requirement?"},
			"clarity": typesafe.Score{
				Instructions: "How precisely is the requirement stated?",
				Criteria:     []any{"Ambiguous", "Mostly clear", "Precise and testable"},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	support, err := response.Noul("support")
	if err != nil {
		panic(err)
	}
	clarity, err := response.Score("clarity")
	if err != nil {
		panic(err)
	}
	fmt.Println(support.Noul, clarity.Score, response.Model)
}
