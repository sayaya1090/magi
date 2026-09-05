#!/bin/bash
# A stand-in for the Antigravity CLI that speaks its stream-json dialect (shapes measured on
# 2026-09-05 against agy itself). `models` and `--version` answer like the real one; anything
# else is a stream-json session: one user event in → thinking, two prose chunks, a result out.
# Turn N answers "turnN", so a test can tell "same child, second turn" from "new child".
case " $* " in
  *" models "*) printf 'gemini\tGemini 3.8 Flash (High)\ngemini\tGemini 3.8 Pro (High)\n'; exit 0;;
  *" --version "*) echo "fake-agy 0.0"; exit 0;;
esac
echo '{"event":"init","conversation_id":"c1","init":{"cwd":"/","tools":[]}}'
n=0
while IFS= read -r line; do
  n=$((n+1))
  echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":0,"state":"DONE","step_type":"user_input"}}'
  echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"ACTIVE","step_type":"thinking","text_delta":"hmm"}}'
  if [[ "$line" == *CALLME* ]]; then
    echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"Sure.\nTOOL_CALL {\"name\":\"list_slides\",\"arguments\":{}}"}}'
    echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"\n","usage":{"input_tokens":10,"output_tokens":5,"thinking_tokens":1,"cache_read_tokens":4}}}'
  else
    echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"hello "}}'
    sleep 0.3
    echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"turn'"$n"'","usage":{"input_tokens":10,"output_tokens":2,"thinking_tokens":1,"cache_read_tokens":4}}}'
  fi
  echo '{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"x","num_turns":'"$n"',"usage":{"input_tokens":10,"output_tokens":2,"thinking_tokens":1,"cache_read_tokens":4}}}'
done
