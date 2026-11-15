#!/bin/bash
# A stand-in for the Claude Code CLI (`claude`) speaking its stream-json dialect.
case " $* " in
  *" --version "*) echo "fake-claude 2.1.306"; exit 0;;
esac
n=0
pending=
while IFS= read -r line; do
  n=$((n+1))
  if [[ "$line" == *TRIGGER_SAFETY* ]]; then
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"TOOL_CALL {\"name\":\"bash\""}}}'
    echo '{"type":"system","subtype":"informational","content":"Opus 5.5'\''s safeguards stopped the response above · continuing once with that noted"}'
    echo '{"type":"user","isMeta":true,"message":{"role":"user","content":"Your response above was stopped by a safety classifier — this is not a tool or API error."}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"fallback_turn'"$n"'\n"}}}'
  elif [[ "$line" == *"corrected, each one valid JSON"* ]]; then
    if [[ "$pending" == fix ]]; then
      echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"retry thinking"}}}'
      echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Sorry, here it is:\nTOOL_CALL {\"name\":\"list_slides\",\"arguments\":{\"deck\":\"main\"}}\n"}}}'
    elif [[ "$pending" == prose ]]; then
      echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"NOT_A_CALL"}}}'
    else
      echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"TOOL_CALL {\"name\":\"list_slides\",\"arguments\":{\"deck\":\"main}}\n"}}}'
    fi
    pending=
  elif [[ "$line" == *BROKEN_CALL* || "$line" == *BROKEN_TWICE* ]]; then
    if [[ "$line" == *BROKEN_CALL* ]]; then pending=fix; else pending=again; fi
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Checking the deck.\nTOOL_CALL {\"name\":\"list_slides\",\"arguments\":{\"deck\":\"main}}\n"}}}'
  elif [[ "$line" == *MENTION_CALL* ]]; then
    pending=prose
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"The format is `TOOL_CALL {...}` per line.\n"}}}'
  elif [[ "$line" == *MIDLINE_CALL* ]]; then
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Let me look. TOOL_C"}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ALL {\"name\":\"list_slides\",\"arguments\":{}}\n"}}}'
  elif [[ "$line" == *NATIVE_INVOKE* ]]; then
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Checking.\n<invoke name=\"list_slides\">\n<parameter name=\"deck\">main</parameter>\n"}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"<parameter name=\"limit\">3</parameter>\n</invoke>\n"}}}'
  elif [[ "$line" == *CALLME* ]]; then
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Sure.\nTOOL_CALL {\"name\":\"list_slides\",\"arguments\":{}}\n"}}}'
  elif [[ "$line" == *OTHER_PROVIDER_ANSWER* && "$line" == *OTHER_PROVIDER_ARG* ]]; then
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"caught_up_turn'"$n"'\n"}}}'
  else
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello turn'"$n"'\n"}}}'
  fi
  echo '{"type":"result","subtype":"success","is_error":false,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":4,"cache_creation_input_tokens":0},"modelUsage":{"claude-opus-5-5":{"contextWindow":1000000}}}'
done
