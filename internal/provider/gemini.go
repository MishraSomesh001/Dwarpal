package provider
import(
	"encoding/json"
	"fmt"
)
//OpenAI Struct
type OpenAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIChatRequest struct {
	Messages []OpenAIChatMessage `json:"messages"`
}

type OpenAIResponse struct {
	Object  string `json:"object"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage  `json:"usage"`
}

type OpenAIChoice struct {
	Index        int                `json:"index"`
	Message      OpenAIChatMessage  `json:"message"`
	FinishReason string             `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

//gemini Struct

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiContent struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Role  string       `json:"role"`
			Parts []GeminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
		Index        int    `json:"index"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func TranslateOpenAIToGemini(oaiReqBody []byte) ([]byte, error) {
	var oaiReq OpenAIChatRequest
	if err := json.Unmarshal(oaiReqBody, &oaiReq); err != nil {
		return nil, err
	}

	var geminiReq GeminiRequest
	geminiReq.Contents = make([]GeminiContent, len(oaiReq.Messages))

	for i, msg := range oaiReq.Messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model" // Gemini uses "model" instead of "assistant"
		}

		geminiReq.Contents[i] = GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: msg.Content}},
		}
	}

	return json.Marshal(geminiReq)
}

func TranslateGeminiToOpenAI(geminiRespBody []byte) ([]byte, error) {
	var geminiResp GeminiResponse
	if err := json.Unmarshal(geminiRespBody, &geminiResp); err != nil {
		return nil, err
	}

	if len(geminiResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates returned from Gemini")
	}

	candidate := geminiResp.Candidates[0]
	text := ""
	if len(candidate.Content.Parts) > 0 {
		text = candidate.Content.Parts[0].Text
	}

	var oaiResp OpenAIResponse
	oaiResp.Object = "chat.completion"
	oaiResp.Choices = []OpenAIChoice{
		{
			Index: candidate.Index,
			Message: OpenAIChatMessage{
				Role:    "assistant",
				Content: text,
			},
			FinishReason: "stop", // Standard OpenAI finish reason
		},
	}

	oaiResp.Usage = OpenAIUsage{
		PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
		CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
		TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
	}

	return json.Marshal(oaiResp)
}
