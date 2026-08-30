package provider

import (
	"context"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

type fakeCompletionClient struct {
	err      error
	last     anyllm.CompletionParams
	response *anyllm.ChatCompletion
}

func (f *fakeCompletionClient) Completion(_ context.Context, params anyllm.CompletionParams) (*anyllm.ChatCompletion, error) {
	f.last = params
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

type fakeResponsesClient struct {
	err      error
	last     anyllm.ResponsesParams
	response *anyllm.ResponsesResult
}

func (f *fakeResponsesClient) Responses(_ context.Context, params anyllm.ResponsesParams) (*anyllm.ResponsesResult, error) {
	f.last = params
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func completionFromContent(content string) *anyllm.ChatCompletion {
	return &anyllm.ChatCompletion{
		Choices: []anyllm.Choice{{
			Message: anyllm.Message{Role: anyllm.RoleAssistant, Content: content},
		}},
	}
}
