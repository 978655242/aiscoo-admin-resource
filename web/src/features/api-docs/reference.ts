/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export type ApiDoc = {
  id: string
  title: string
  group: string
  summary: string
  method?: 'GET' | 'POST'
  path?: string
  parameters?: {
    name: string
    type: string
    required?: boolean
    description: string
  }[]
  request?: { language: string; code: string }[]
  response?: string
  notes?: string[]
  stream?: { request: string; response: string; description: string }
  related?: { method: string; path: string; description: string }[]
}

const modelParameter = {
  name: 'model',
  type: 'string',
  required: true,
  description: 'Model ID available to your API key for this endpoint.',
}
const exampleNote =
  'Response examples are illustrative excerpts; fields vary by model and provider.'
const modelNote =
  'Set API_KEY in your environment and replace YOUR_MODEL_ID with a model that supports this endpoint.'

export const API_DOCS: ApiDoc[] = [
  {
    id: 'models',
    title: 'Models',
    group: 'API basics',
    summary: 'List the models available to the current API key.',
    method: 'GET',
    path: '/v1/models',
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/models" \\
  -H "Authorization: Bearer $API_KEY"`,
      },
    ],
    response: `{
  "object": "list",
  "data": [{"id": "YOUR_MODEL_ID", "object": "model", "created": 1700000000, "owned_by": "provider", "supported_endpoint_types": ["openai"]}],
  "success": true
}`,
    notes: [
      'A Bearer API key is required to list models.',
      'Availability depends on API key permissions and enabled models; supported_endpoint_types describes compatible endpoint families.',
      exampleNote,
    ],
  },
  {
    id: 'chat-completions',
    title: 'Chat Completions',
    group: 'Text and multimodal',
    summary: 'Generate an assistant reply from a conversation of messages.',
    method: 'POST',
    path: '/v1/chat/completions',
    parameters: [
      modelParameter,
      {
        name: 'messages',
        type: 'array',
        required: true,
        description: 'Conversation messages with role and content.',
      },
      {
        name: 'stream',
        type: 'boolean',
        description: 'Set to true to receive server-sent events.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/chat/completions" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'`,
      },
      {
        language: 'python',
        code: `import os
from openai import OpenAI

client = OpenAI(api_key=os.environ["API_KEY"], base_url="__OPENAI_BASE__")
reply = client.chat.completions.create(
    model="YOUR_MODEL_ID", messages=[{"role": "user", "content": "Hello"}]
)
print(reply.choices[0].message.content)`,
      },
    ],
    response: `{
  "id": "chatcmpl_example", "object": "chat.completion", "created": 1700000000, "model": "YOUR_MODEL_ID",
  "choices": [{"index": 0, "message": {"role": "assistant", "content": "Hello!"}, "finish_reason": "stop"}],
  "usage": {"prompt_tokens": 8, "completion_tokens": 2, "total_tokens": 10}
}`,
    stream: {
      description:
        'Append delta.content as text arrives; finish_reason ends a choice and [DONE] ends the stream. If supported, stream_options.include_usage adds a final usage chunk with an empty choices array.',
      request: `curl -N "__API_ORIGIN__/v1/chat/completions" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Hello"}],"stream":true,"stream_options":{"include_usage":true}}'`,
      response: `data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1700000000,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"content":"Hello!"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1700000000,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1700000000,"model":"YOUR_MODEL_ID","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}

data: [DONE]`,
    },
    notes: [
      modelNote,
      'Image inputs, tool calls, structured output, and reasoning options depend on the selected model.',
      exampleNote,
    ],
  },
  {
    id: 'responses',
    title: 'Responses',
    group: 'Text and multimodal',
    summary: 'Generate a response from text or structured input items.',
    method: 'POST',
    path: '/v1/responses',
    parameters: [
      modelParameter,
      {
        name: 'input',
        type: 'string | array',
        required: true,
        description: 'Text input or an array of structured input items.',
      },
      {
        name: 'stream',
        type: 'boolean',
        description: 'Set to true to receive typed response events.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/responses" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","input":"Hello"}'`,
      },
      {
        language: 'python',
        code: `import os
from openai import OpenAI

client = OpenAI(api_key=os.environ["API_KEY"], base_url="__OPENAI_BASE__")
response = client.responses.create(model="YOUR_MODEL_ID", input="Hello")
print(response.output_text)`,
      },
    ],
    response: `{
  "id": "resp_example", "object": "response", "created_at": 1700000000, "status": "completed", "model": "YOUR_MODEL_ID",
  "output": [{"type": "message", "id": "msg_example", "status": "completed", "role": "assistant", "content": [{"type": "output_text", "text": "Hello!", "annotations": []}]}],
  "usage": {"input_tokens": 8, "output_tokens": 2, "total_tokens": 10}
}`,
    stream: {
      description:
        'Read response.output_text.delta and handle response.completed, response.failed, or response.incomplete as terminal events. This excerpt omits intermediate item events.',
      request: `curl -N "__API_ORIGIN__/v1/responses" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","input":"Hello","stream":true}'`,
      response: `event: response.output_text.delta
data: {"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_example","output_index":0,"content_index":0,"delta":"Hello!"}

event: response.output_text.done
data: {"type":"response.output_text.done","sequence_number":4,"item_id":"msg_example","output_index":0,"content_index":0,"text":"Hello!"}

event: response.completed
data: {"type":"response.completed","sequence_number":7,"response":{"id":"resp_example","object":"response","status":"completed","output":[{"type":"message","id":"msg_example","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello!","annotations":[]}]}],"usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}`,
    },
    notes: [
      modelNote,
      'Tools, multimodal input, and previous_response_id require compatible model and upstream support; the Python SDK exposes output_text as a convenience property over output items.',
      exampleNote,
    ],
  },
  {
    id: 'messages',
    title: 'Anthropic Messages',
    group: 'Text and multimodal',
    summary: 'Send a conversation using the Anthropic Messages protocol.',
    method: 'POST',
    path: '/v1/messages',
    parameters: [
      modelParameter,
      {
        name: 'messages',
        type: 'array',
        required: true,
        description:
          'User and assistant messages; system instructions use the top-level system field.',
      },
      {
        name: 'max_tokens',
        type: 'integer',
        required: true,
        description: 'Maximum output tokens for the response.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/messages" \\
  -H "x-api-key: $API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  --json '{"model":"YOUR_MODEL_ID","max_tokens":256,"messages":[{"role":"user","content":"Hello"}]}'`,
      },
    ],
    response: `{
  "id": "msg_example", "type": "message", "role": "assistant", "model": "YOUR_MODEL_ID",
  "content": [{"type": "text", "text": "Hello!"}], "stop_reason": "end_turn",
  "usage": {"input_tokens": 8, "output_tokens": 2}
}`,
    notes: [
      modelNote,
      'Send x-api-key and anthropic-version headers; stream: true enables Anthropic events such as content_block_delta and message_stop.',
      exampleNote,
    ],
  },
  {
    id: 'gemini',
    title: 'Gemini generateContent',
    group: 'Text and multimodal',
    summary: 'Generate content using the Gemini request and response format.',
    method: 'POST',
    path: '/v1beta/models/{model}:generateContent',
    parameters: [
      {
        name: 'contents',
        type: 'array',
        required: true,
        description: 'Conversation turns containing role and parts.',
      },
      {
        name: 'generationConfig',
        type: 'object',
        description: 'Optional generation settings supported by the model.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1beta/models/YOUR_MODEL_ID:generateContent" \\
  -H "x-goog-api-key: $API_KEY" \\
  --json '{"contents":[{"role":"user","parts":[{"text":"Hello"}]}]}'`,
      },
    ],
    response: `{
  "candidates": [{"content": {"role": "model", "parts": [{"text": "Hello!"}]}, "finishReason": "STOP", "index": 0}],
  "usageMetadata": {"promptTokenCount": 8, "candidatesTokenCount": 2, "totalTokenCount": 10}
}`,
    related: [
      {
        method: 'POST',
        path: '/v1beta/models/{model}:streamGenerateContent?alt=sse',
        description:
          'Stream Gemini response objects as server-sent events using the same body and x-goog-api-key header.',
      },
    ],
    notes: [
      modelNote,
      'Put the model ID in the URL and the API key in x-goog-api-key; multimodal parts and generation settings depend on the model.',
      exampleNote,
    ],
  },
  {
    id: 'embeddings',
    title: 'Embeddings',
    group: 'Media and utilities',
    summary: 'Convert text into vectors for similarity search and retrieval.',
    method: 'POST',
    path: '/v1/embeddings',
    parameters: [
      modelParameter,
      {
        name: 'input',
        type: 'string | array',
        required: true,
        description: 'A text string or a batch of text strings to embed.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/embeddings" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","input":"A short sentence.","encoding_format":"float"}'`,
      },
    ],
    response: `{
  "object": "list", "model": "YOUR_MODEL_ID",
  "data": [{"object": "embedding", "index": 0, "embedding": [0.12, -0.03, 0.08]}],
  "usage": {"prompt_tokens": 4, "total_tokens": 4}
}`,
    notes: [
      modelNote,
      'The example vector is shortened; vector dimensions, batch limits, encoding_format, and optional dimensions depend on the model.',
      exampleNote,
    ],
  },
  {
    id: 'rerank',
    title: 'Rerank',
    group: 'Media and utilities',
    summary: 'Rank candidate documents by relevance to a query.',
    method: 'POST',
    path: '/v1/rerank',
    parameters: [
      modelParameter,
      {
        name: 'query',
        type: 'string',
        required: true,
        description: 'The search query used to rank the documents.',
      },
      {
        name: 'documents',
        type: 'array',
        required: true,
        description: 'Candidate text documents to compare with the query.',
      },
      {
        name: 'top_n',
        type: 'integer',
        description: 'Maximum number of ranked results to return.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/rerank" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","query":"What is a vector?","documents":["A vector is an ordered list of numbers.","It is raining."],"top_n":1}'`,
      },
    ],
    response: `{
  "results": [{"index": 0, "relevance_score": 0.98}],
  "usage": {"prompt_tokens": 20, "total_tokens": 20}
}`,
    notes: [
      modelNote,
      'Result indices refer to the original documents array; return_documents: true requests document content when supported.',
      exampleNote,
    ],
  },
  {
    id: 'images',
    title: 'Images',
    group: 'Media and utilities',
    summary:
      'Generate images from a text prompt with a compatible image model.',
    method: 'POST',
    path: '/v1/images/generations',
    parameters: [
      modelParameter,
      {
        name: 'prompt',
        type: 'string',
        required: true,
        description: 'A text description of the image to generate.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/images/generations" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","prompt":"A lighthouse at sunrise"}'`,
      },
    ],
    response: `{
  "created": 1700000000,
  "data": [{"url": "https://example.com/generated-image.png"}]
}`,
    related: [
      {
        method: 'POST',
        path: '/v1/images/edits',
        description:
          'Edit images using multipart/form-data with model, prompt, image, and an optional supported mask; let cURL set the multipart boundary.',
      },
    ],
    notes: [
      modelNote,
      'Supported size, quality, n, and response_format values depend on the model; results can contain url or b64_json, and temporary URLs expire.',
      exampleNote,
    ],
  },
  {
    id: 'audio',
    title: 'Audio',
    group: 'Media and utilities',
    summary: 'Generate speech audio from text with a compatible speech model.',
    method: 'POST',
    path: '/v1/audio/speech',
    parameters: [
      modelParameter,
      {
        name: 'input',
        type: 'string',
        required: true,
        description: 'The text to synthesize as speech.',
      },
      {
        name: 'voice',
        type: 'string',
        required: true,
        description: 'A voice supported by the selected speech model.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/audio/speech" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","input":"Hello from aiscoo.","voice":"YOUR_VOICE_ID","response_format":"mp3"}' \\
  --output speech.mp3`,
      },
    ],
    related: [
      {
        method: 'POST',
        path: '/v1/audio/transcriptions',
        description:
          'Transcribe audio using multipart/form-data with model and file; optional language and response_format depend on the model, and the default JSON result contains text.',
      },
      {
        method: 'POST',
        path: '/v1/audio/translations',
        description:
          'Translate speech into English using multipart/form-data with model and file; a compatible translation model is required, and the default JSON result contains text.',
      },
    ],
    notes: [
      modelNote,
      'Speech returns binary audio, saved as speech.mp3 here; replace YOUR_VOICE_ID with a supported voice, and check model support for formats and upload limits.',
      'For uploads, use cURL -F "model=YOUR_MODEL_ID" -F "file=@audio.mp3" and let cURL set the multipart boundary.',
    ],
  },
  {
    id: 'videos',
    title: 'Videos',
    group: 'Media and utilities',
    summary:
      'Submit a video generation task and poll its status until completion.',
    method: 'POST',
    path: '/v1/videos',
    parameters: [
      modelParameter,
      {
        name: 'prompt',
        type: 'string',
        required: true,
        description: 'A text description of the video to generate.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/videos" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","prompt":"Waves rolling onto a quiet beach"}'`,
      },
    ],
    response: `{
  "id": "video_example", "object": "video", "model": "YOUR_MODEL_ID",
  "status": "queued", "progress": 0, "created_at": 1700000000
}`,
    related: [
      {
        method: 'GET',
        path: '/v1/videos/{task_id}',
        description:
          'Poll the returned id using the same Bearer API key; states are queued, in_progress, completed, or failed, with error.code and error.message on failure.',
      },
      {
        method: 'GET',
        path: '/v1/videos/{task_id}/content',
        description:
          'Download the completed video with the same Bearer API key when the selected model supports this content endpoint.',
      },
    ],
    notes: [
      modelNote,
      'Video endpoints require an enabled compatible model; JSON or multipart input, duration, size, reference images, and content retrieval depend on that model.',
      exampleNote,
    ],
  },
  {
    id: 'moderations',
    title: 'Moderations',
    group: 'Media and utilities',
    summary:
      'Classify input for harmful content with a compatible moderation model.',
    method: 'POST',
    path: '/v1/moderations',
    parameters: [
      modelParameter,
      {
        name: 'input',
        type: 'string | array',
        required: true,
        description: 'Text or a batch of text strings to classify.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/moderations" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","input":"Have a nice day."}'`,
      },
    ],
    response: `{
  "id": "modr_example", "model": "YOUR_MODEL_ID",
  "results": [{"flagged": false, "categories": {"violence": false}, "category_scores": {"violence": 0.001}}]
}`,
    notes: [
      modelNote,
      'The example shows one category; returned categories, scores, and support for image input depend on the model.',
      exampleNote,
    ],
  },
]
