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
export type ApiCodeSample = {
  language: string
  code: string
  label?: string
}

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
  request?: ApiCodeSample[]
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

export const API_CAPABILITIES = [
  {
    title: 'Text generation',
    description: 'Chat, reasoning, code generation, and tool calling.',
    docs: [
      'chat-completions',
      'responses',
      'messages',
      'gemini',
      'completions',
    ],
  },
  {
    title: 'Multimodal understanding',
    description: 'Understand images, audio, and video with a compatible model.',
    docs: ['multimodal'],
  },
  {
    title: 'Image generation and editing',
    description: 'Generate and edit images, including Midjourney tasks.',
    docs: ['images', 'image-edits', 'midjourney'],
  },
  {
    title: 'Video generation and editing',
    description:
      'Generate videos from text or images and retrieve completed tasks.',
    docs: ['videos'],
  },
  {
    title: 'Speech and realtime audio',
    description:
      'Text to speech, transcription, speech translation, and realtime sessions.',
    docs: ['audio', 'transcriptions', 'translations', 'realtime'],
  },
  {
    title: 'Music and lyrics',
    description: 'Generate songs or lyrics through the SunoAPI task plugin.',
    docs: ['music'],
  },
  {
    title: 'Embeddings',
    description:
      'Convert text to vectors for semantic retrieval and similarity search.',
    docs: ['embeddings'],
  },
  {
    title: 'Rerank',
    description: 'Sort candidate documents by relevance to a query.',
    docs: ['rerank'],
  },
  {
    title: 'Content moderation',
    description: 'Check content against the selected moderation model.',
    docs: ['moderations'],
  },
  {
    title: 'Search and web fetching',
    description: 'Search the web and fetch pages with compatible search tools.',
    docs: ['search', 'fetch'],
  },
]

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
      {
        language: 'bash',
        label: 'Tool calling',
        code: `curl "__API_ORIGIN__/v1/chat/completions" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"What is the weather in Paris?"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get the weather in a city","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}],"tool_choice":"auto"}'`,
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
      'For tool calls, execute choices[].message.tool_calls in your application, append the assistant message and tool results with matching tool_call_id values, then request the next reply.',
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
    related: [
      {
        method: 'GET',
        path: '/v1/responses/{response_id}',
        description:
          'Retrieve a persisted task-plugin Response by its returned ID; ordinary relayed Responses are not stored by this endpoint.',
      },
      {
        method: 'GET',
        path: '/v1/responses',
        description:
          'Upgrade to WebSocket and send response.create with model and input. The selected channel must enable Responses WebSocket; each connection keeps one model and one active generation at a time.',
      },
      {
        method: 'POST',
        path: '/v1/responses/compact',
        description:
          'Compact conversation input with a model and upstream that support the compact operation.',
      },
    ],
    notes: [
      modelNote,
      'Tools, multimodal input, and previous_response_id require compatible model and upstream support; the Python SDK exposes output_text as a convenience property over output items.',
      'Task plugins declare sync, stream, and background modes separately. With supported background=true, poll the returned Response ID until completed or failed; unsupported modes are rejected.',
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
    id: 'completions',
    title: 'Text Completions',
    group: 'Text and multimodal',
    summary: 'Complete a text prompt using the legacy Completions format.',
    method: 'POST',
    path: '/v1/completions',
    parameters: [
      modelParameter,
      {
        name: 'prompt',
        type: 'string | array',
        required: true,
        description: 'The text prompt to complete.',
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
        code: `curl "__API_ORIGIN__/v1/completions" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","prompt":"Complete this sentence: The sky is"}'`,
      },
    ],
    response: `{
  "id": "cmpl_example", "object": "text_completion", "model": "YOUR_MODEL_ID",
  "choices": [{"index": 0, "text": " blue.", "finish_reason": "stop"}]
}`,
    notes: [
      modelNote,
      'This legacy endpoint requires a completion-compatible model. Read choices[].text; chat models normally use Chat Completions or Responses.',
      exampleNote,
    ],
  },
  {
    id: 'responses-compact',
    title: 'Responses compaction',
    group: 'Text and multimodal',
    summary: 'Compact conversation history with a compatible Responses model.',
    method: 'POST',
    path: '/v1/responses/compact',
    parameters: [
      modelParameter,
      {
        name: 'input',
        type: 'array',
        required: true,
        description:
          'Conversation input items to compact for subsequent requests.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/responses/compact" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","input":[{"role":"user","content":"We are planning a trip to Paris."},{"role":"assistant","content":"What dates are you considering?"}]}'`,
      },
    ],
    notes: [
      modelNote,
      'Compaction is non-streaming and requires explicit upstream support. Keep the returned output items in the format required by that upstream; they are not ordinary assistant text.',
      'Do not assume that Chat Completions tools or generation settings apply to compaction. The accepted input and output item shapes follow the upstream compact API.',
    ],
  },
  {
    id: 'multimodal',
    title: 'Multimodal inputs',
    group: 'Text and multimodal',
    summary:
      'Send image, audio, or video inputs through a compatible conversation API.',
    method: 'POST',
    path: '/v1/chat/completions',
    parameters: [
      modelParameter,
      {
        name: 'messages[].content',
        type: 'array',
        required: true,
        description:
          'Typed content parts containing text and supported media inputs.',
      },
    ],
    request: [
      {
        language: 'bash',
        label: 'Image input',
        code: `curl "__API_ORIGIN__/v1/chat/completions" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":[{"type":"text","text":"Describe this image."},{"type":"image_url","image_url":{"url":"https://example.com/image.jpg"}}]}]}'`,
      },
      {
        language: 'python',
        label: 'Audio input',
        code: `import base64
import os
from openai import OpenAI

client = OpenAI(api_key=os.environ["API_KEY"], base_url="__OPENAI_BASE__")
with open("audio.wav", "rb") as audio_file:
    data = base64.b64encode(audio_file.read()).decode("ascii")
reply = client.chat.completions.create(
    model="YOUR_MODEL_ID",
    messages=[{"role": "user", "content": [
        {"type": "text", "text": "Summarize this recording."},
        {"type": "input_audio", "input_audio": {"data": data, "format": "wav"}}
    ]}]
)
print(reply.choices[0].message.content)`,
      },
      {
        language: 'bash',
        label: 'Video input',
        code: `curl "__API_ORIGIN__/v1beta/models/YOUR_MODEL_ID:generateContent" \\
  -H "x-goog-api-key: $API_KEY" \\
  --json '{"contents":[{"role":"user","parts":[{"text":"Summarize this video."},{"fileData":{"mimeType":"video/mp4","fileUri":"https://example.com/clip.mp4"}}]}]}'`,
      },
    ],
    response: `{
  "choices": [{"index": 0, "message": {"role": "assistant", "content": "The image shows a lighthouse by the sea."}, "finish_reason": "stop"}]
}`,
    related: [
      {
        method: 'POST',
        path: '/v1/responses',
        description:
          'Responses uses input_text and input_image content parts; other media types require compatible upstream support.',
      },
      {
        method: 'POST',
        path: '/v1beta/models/{model}:generateContent',
        description:
          'Gemini accepts supported inlineData or fileData parts alongside text.',
      },
    ],
    notes: [
      modelNote,
      'Each example needs a model that accepts that media type. Video uses the Gemini endpoint shown in its example, rather than the Chat Completions endpoint.',
      'Replace example media URLs with accessible files. Accepted URLs, data URLs, MIME types, file sizes, and media duration depend on the provider.',
      'These inputs analyze existing media; use image, video, or speech generation endpoints to create new media.',
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
      {
        language: 'bash',
        label: 'Batch input',
        code: `curl "__API_ORIGIN__/v1/embeddings" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","input":["A short sentence.","A second sentence."],"encoding_format":"float"}'`,
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
      'For batch input, data[].index identifies the corresponding input item. Store vectors with their model and dimensions, and embed queries with the same model.',
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
      {
        language: 'bash',
        label: 'Base64 result',
        code: `curl "__API_ORIGIN__/v1/images/generations" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","prompt":"A lighthouse at sunrise","response_format":"b64_json"}'`,
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
    id: 'image-edits',
    title: 'Image edits',
    group: 'Media and utilities',
    summary: 'Edit an existing image with a prompt and optional mask.',
    method: 'POST',
    path: '/v1/images/edits',
    parameters: [
      modelParameter,
      {
        name: 'image',
        type: 'file',
        required: true,
        description: 'The source image uploaded as a multipart file.',
      },
      {
        name: 'prompt',
        type: 'string',
        required: true,
        description: 'Describe the requested changes to the image.',
      },
      {
        name: 'mask',
        type: 'file',
        description:
          'An optional edit mask, supported only by compatible models.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/images/edits" \\
  -H "Authorization: Bearer $API_KEY" \\
  -F "model=YOUR_MODEL_ID" \\
  -F "prompt=Replace the background with a sunset" \\
  -F "image=@image.png"`,
      },
    ],
    response: `{
  "created": 1700000000,
  "data": [{"url": "https://example.com/edited-image.png"}]
}`,
    notes: [
      modelNote,
      'Let cURL set the multipart boundary. Mask semantics, image formats, multiple reference images, and JSON input depend on the selected model or plugin.',
      'With response_format=b64_json, read data[].b64_json when supported; URL responses may expire, so save generated images promptly.',
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
    id: 'transcriptions',
    title: 'Audio transcription',
    group: 'Media and utilities',
    summary: 'Convert a recorded audio file to text.',
    method: 'POST',
    path: '/v1/audio/transcriptions',
    parameters: [
      modelParameter,
      {
        name: 'file',
        type: 'file',
        required: true,
        description: 'The audio file uploaded with multipart/form-data.',
      },
      {
        name: 'language',
        type: 'string',
        description: 'Optional source language code supported by the model.',
      },
      {
        name: 'response_format',
        type: 'string',
        description:
          'The transcript format; supported values depend on the model.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/audio/transcriptions" \\
  -H "Authorization: Bearer $API_KEY" \\
  -F "model=YOUR_MODEL_ID" \\
  -F "file=@audio.mp3" \\
  -F "response_format=json"`,
      },
    ],
    response: `{"text": "Welcome to aiscoo."}`,
    notes: [
      modelNote,
      'Let cURL set the multipart boundary. Audio formats, upload limits, timestamps, speaker labels, and streaming support depend on the selected transcription model.',
      'Transcription preserves the spoken language; use audio translations when English output is required.',
      exampleNote,
    ],
  },
  {
    id: 'translations',
    title: 'Audio translation',
    group: 'Media and utilities',
    summary: 'Translate recorded speech into English text.',
    method: 'POST',
    path: '/v1/audio/translations',
    parameters: [
      modelParameter,
      {
        name: 'file',
        type: 'file',
        required: true,
        description: 'The audio file uploaded with multipart/form-data.',
      },
      {
        name: 'response_format',
        type: 'string',
        description:
          'The transcript format; supported values depend on the model.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/audio/translations" \\
  -H "Authorization: Bearer $API_KEY" \\
  -F "model=YOUR_MODEL_ID" \\
  -F "file=@speech.mp3" \\
  -F "response_format=json"`,
      },
    ],
    response: `{"text": "Good morning. How can I help you?"}`,
    notes: [
      modelNote,
      'This endpoint returns English text, not translated audio. Use a model that explicitly supports audio translation; transcription-only models are not sufficient.',
      exampleNote,
    ],
  },
  {
    id: 'realtime',
    title: 'Realtime audio',
    group: 'Media and utilities',
    summary:
      'Exchange text and audio events over a realtime WebSocket connection.',
    method: 'GET',
    path: '/v1/realtime',
    parameters: [
      {
        ...modelParameter,
        description:
          'A realtime-capable model ID supplied in the WebSocket URL query.',
      },
    ],
    request: [
      {
        language: 'javascript',
        code: `import WebSocket from "ws";

const url = new URL("__API_ORIGIN__/v1/realtime");
url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
url.searchParams.set("model", "YOUR_MODEL_ID");
const socket = new WebSocket(url, {
  headers: { Authorization: "Bearer " + process.env.API_KEY }
});
socket.on("open", () => {
  socket.send(JSON.stringify({
    type: "session.update",
    session: { instructions: "Answer concisely." }
  }));
});
socket.on("message", (data) => console.log(JSON.parse(data.toString())));
socket.on("error", console.error);`,
      },
    ],
    response: `{
  "type": "session.updated",
  "session": {"instructions": "Answer concisely."}
}`,
    notes: [
      modelNote,
      'Run this example on a server with the ws package installed. Use wss for HTTPS sites and keep API keys out of browser code.',
      'The model and upstream must support Realtime; ordinary chat models are not interchangeable. Session settings and event names can differ between API versions.',
      'For microphone input, send input_audio_buffer.append events with encoded audio, then commit the buffer and request a response as required by the upstream. Handle audio deltas, completion, and error events.',
      exampleNote,
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
      {
        language: 'bash',
        label: 'Poll task',
        code: `curl "__API_ORIGIN__/v1/videos/YOUR_TASK_ID" \\
  -H "Authorization: Bearer $API_KEY"`,
      },
      {
        language: 'bash',
        label: 'Download result',
        code: `curl "__API_ORIGIN__/v1/videos/YOUR_TASK_ID/content" \\
  -H "Authorization: Bearer $API_KEY" \\
  --output video.mp4`,
      },
      {
        language: 'bash',
        label: 'Image to video',
        code: `curl "__API_ORIGIN__/v1/videos" \\
  -H "Authorization: Bearer $API_KEY" \\
  -F "model=YOUR_MODEL_ID" \\
  -F "prompt=Animate this scene with gentle camera movement" \\
  -F "input_reference=@reference.png"`,
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
      {
        method: 'POST / GET',
        path: '/v1/video/generations[/{task_id}]',
        description:
          'Legacy video submission and task lookup use a different response format; they are not interchangeable aliases of /v1/videos.',
      },
    ],
    notes: [
      modelNote,
      'Video endpoints require an enabled compatible model; JSON or multipart input, duration, size, reference images, and content retrieval depend on that model.',
      'Replace YOUR_TASK_ID with the returned id. Poll until completed or failed before downloading; on failure, inspect error.code and error.message.',
      'Provider-specific editing, extension, keyframe, and reference inputs use compatible plugin routes, such as /xai/v1/videos/edits or /vidu/ent/v2/start-end2video.',
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
  {
    id: 'music',
    title: 'Music and lyrics',
    group: 'Media and utilities',
    summary:
      'Submit music or lyrics generation and poll the SunoAPI task result.',
    method: 'POST',
    path: '/suno/submit/{action}',
    parameters: [
      {
        name: 'action',
        type: 'string',
        required: true,
        description:
          'Use music or lyrics in the URL path; no model field is required.',
      },
      {
        name: 'gpt_description_prompt',
        type: 'string',
        description:
          'A description for music generation, subject to upstream requirements.',
      },
      {
        name: 'prompt',
        type: 'string',
        description:
          'Required for lyrics generation; music prompt semantics depend on the upstream.',
      },
    ],
    request: [
      {
        language: 'bash',
        label: 'Generate music',
        code: `curl "__API_ORIGIN__/suno/submit/music" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"gpt_description_prompt":"An upbeat acoustic song about spring"}'`,
      },
      {
        language: 'bash',
        label: 'Generate lyrics',
        code: `curl "__API_ORIGIN__/suno/submit/lyrics" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"prompt":"Write lyrics about spring"}'`,
      },
      {
        language: 'bash',
        label: 'Poll task',
        code: `curl "__API_ORIGIN__/suno/fetch/YOUR_TASK_ID" \\
  -H "Authorization: Bearer $API_KEY"`,
      },
    ],
    response: `{
  "code": "success",
  "message": "",
  "data": "task_example"
}`,
    related: [
      {
        method: 'GET',
        path: '/suno/fetch/{task_id}',
        description:
          'The submit response data is the task ID. A query returns data.status, data.fail_reason, and provider results in data.data; songs may contain audio_url and lyrics may contain text.',
      },
      {
        method: 'POST',
        path: '/suno/fetch',
        description:
          'Query several tasks with a JSON body containing ids: an array of public task IDs.',
      },
      {
        method: 'POST',
        path: '/v1/responses',
        description:
          'The SunoAPI plugin also accepts compatible Responses requests using suno_music or suno_lyrics when the plugin and model are enabled.',
      },
    ],
    notes: [
      'Requires the enabled SunoAPI plugin and a compatible channel. This plugin targets a third-party SunoAPI service, rather than a standard OpenAI audio API.',
      'A successful submission or query does not mean generation has completed. Poll until data.status is SUCCESS or FAILURE; inspect data.fail_reason on failure.',
      exampleNote,
    ],
  },
  {
    id: 'midjourney',
    title: 'Midjourney tasks',
    group: 'Media and utilities',
    summary:
      'Create images and run related Midjourney actions through a compatible proxy.',
    method: 'POST',
    path: '/mj/submit/imagine',
    parameters: [
      {
        name: 'prompt',
        type: 'string',
        required: true,
        description:
          'The Midjourney image prompt; supported options depend on the proxy.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/mj/submit/imagine" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"prompt":"A lighthouse at sunrise"}'`,
      },
      {
        language: 'bash',
        label: 'Poll task',
        code: `curl "__API_ORIGIN__/mj/task/YOUR_TASK_ID/fetch" \\
  -H "Authorization: Bearer $API_KEY"`,
      },
      {
        language: 'bash',
        label: 'Task action',
        code: `curl "__API_ORIGIN__/mj/submit/action" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"taskId":"YOUR_TASK_ID","customId":"BUTTON_CUSTOM_ID"}'`,
      },
    ],
    response: `{
  "code": 1,
  "description": "Submitted",
  "result": "0741798445574458"
}`,
    related: [
      {
        method: 'GET',
        path: '/mj/task/{id}/fetch',
        description:
          'Read the task ID from result. Queries return the task object directly, with status, progress, imageUrl, failReason, and optional buttons.',
      },
      {
        method: 'POST',
        path: '/mj/submit/describe',
        description:
          'Describe images by submitting base64Array with supported image data URLs.',
      },
      {
        method: 'POST',
        path: '/mj/submit/blend',
        description:
          'Blend reference images supplied in base64Array; input limits depend on the proxy.',
      },
      {
        method: 'POST',
        path: '/mj/submit/action',
        description:
          'Submit taskId and a customId copied from the original task buttons; do not construct button identifiers yourself.',
      },
    ],
    notes: [
      'Requires a compatible Midjourney proxy channel. These routes use the proxy task format, not the OpenAI Images format.',
      'code=1 acknowledges submission. Wait for task status SUCCESS or FAILURE, and inspect failReason on failure. Supported editing and video actions depend on the proxy.',
      exampleNote,
    ],
  },
  {
    id: 'search',
    title: 'Web search',
    group: 'Media and utilities',
    summary: 'Search the web through a compatible search-tools channel.',
    method: 'POST',
    path: '/v1/tools/search',
    parameters: [
      {
        name: 'query',
        type: 'string',
        required: true,
        description: 'The search query passed to the upstream search service.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/tools/search" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"query":"DeepSeek API documentation"}'`,
      },
      {
        language: 'bash',
        label: 'Alpha search',
        code: `curl "__API_ORIGIN__/v1/alpha/search" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"model":"YOUR_MODEL_ID","commands":{"search_query":[{"q":"DeepSeek API documentation"}]}}'`,
      },
    ],
    response: `{
  "results": [{"title": "DeepSeek", "url": "https://deepseek.com/", "rerank_score": 2.1}]
}`,
    related: [
      {
        method: 'POST',
        path: '/v1/alpha/search',
        description:
          'Alpha search requires model and forwards command fields to a compatible upstream. Command syntax and response format are upstream-specific.',
      },
      {
        method: 'POST',
        path: '/v1/tools/fetch',
        description:
          'Fetch a page by URL using the separate web-fetching endpoint.',
      },
    ],
    notes: [
      'The tools/search body does not need model. Routing uses deepseek-search, which must be available to your API key on a channel that supports this endpoint.',
      'Search and fetch tools support compatible DeepSeek, New API, Sub2API, or Advanced Custom channels. Having a chat model alone does not enable these tools.',
      'The Alpha example illustrates a forwarded command shape, not a universal search schema. Alpha search requires compatible Codex, New API, Sub2API, or Advanced Custom support.',
      exampleNote,
    ],
  },
  {
    id: 'fetch',
    title: 'Web fetching',
    group: 'Media and utilities',
    summary:
      'Retrieve page content from a URL through a compatible fetch tool.',
    method: 'POST',
    path: '/v1/tools/fetch',
    parameters: [
      {
        name: 'url',
        type: 'string',
        required: true,
        description:
          'The page URL to retrieve; supported URLs depend on the upstream.',
      },
    ],
    request: [
      {
        language: 'bash',
        code: `curl "__API_ORIGIN__/v1/tools/fetch" \\
  -H "Authorization: Bearer $API_KEY" \\
  --json '{"url":"https://example.com/"}'`,
      },
    ],
    response: `{
  "title": "Example Domain",
  "content": "# Example Domain"
}`,
    notes: [
      'The tools/fetch body does not need model. Routing uses deepseek-fetch, which must be available to your API key on a channel that supports this endpoint.',
      'Search and fetch tools support compatible DeepSeek, New API, Sub2API, or Advanced Custom channels. Having a chat model alone does not enable these tools.',
      'The upstream controls page access, extraction, and result fields. Treat fetched content as external data in your application.',
      exampleNote,
    ],
  },
  {
    id: 'tasks',
    title: 'Tasks and artifacts',
    group: 'API basics',
    summary:
      'Inspect asynchronous tasks and retrieve their generated artifacts.',
    method: 'GET',
    path: '/v1/tasks/{task_id}',
    parameters: [
      {
        name: 'task_id',
        type: 'string',
        required: true,
        description: 'The public task ID returned by a generation endpoint.',
      },
    ],
    request: [
      {
        language: 'bash',
        label: 'Poll task',
        code: `curl "__API_ORIGIN__/v1/tasks/YOUR_TASK_ID" \\
  -H "Authorization: Bearer $API_KEY"`,
      },
      {
        language: 'bash',
        label: 'List artifacts',
        code: `curl "__API_ORIGIN__/v1/tasks/YOUR_TASK_ID/artifacts" \\
  -H "Authorization: Bearer $API_KEY"`,
      },
      {
        language: 'bash',
        label: 'Download result',
        code: `curl "__API_ORIGIN__/v1/tasks/YOUR_TASK_ID/artifacts/YOUR_ARTIFACT_KEY/content" \\
  -H "Authorization: Bearer $API_KEY" \\
  --output result.bin`,
      },
    ],
    response: `{
  "task_id": "task_example",
  "platform": "sora",
  "status": "SUCCESS",
  "progress": "100%",
  "fail_reason": "",
  "created_at": 1700000000,
  "finished_at": 1700000060
}`,
    related: [
      {
        method: 'GET',
        path: '/v1/tasks/{task_id}/artifacts',
        description:
          'Returns task_id and artifacts with key, type, content_url, and optional mime_type. Download an artifact using its returned content_url.',
      },
      {
        method: 'GET / HEAD',
        path: '/v1/tasks/{task_id}/artifacts/{artifact_key}/content',
        description:
          'Returns artifact bytes. An unfinished artifact returns HTTP 409; signed content URLs should be treated as private access links.',
      },
      {
        method: 'POST',
        path: '/v1/tasks/{plugin_key}',
        description:
          'Advanced generic submission: send model and plugin driver fields in a flat JSON body. Compatibility depends on the driver; prefer the documented protocol or native plugin endpoint.',
      },
    ],
    notes: [
      'Use the same API key to query your tasks. Terminal task states are SUCCESS and FAILURE; read fail_reason when a task fails.',
      'Artifact availability depends on the plugin. Responses, video, Suno, and Midjourney query formats differ; use the matching reference instead of assuming identical envelopes.',
      'Submitting again creates another generation request. After a timeout, check the existing task before resubmitting.',
      exampleNote,
    ],
  },
]
