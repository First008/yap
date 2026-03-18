#!/bin/bash
# Test all OpenAI TTS voices with a review-style sentence.

TEXT="This file adds push to talk support. The state machine looks solid, but there is a missing null check on line forty two that could cause a crash."

VOICES=(alloy ash ballad cedar coral echo fable marin nova onyx sage shimmer)

for voice in "${VOICES[@]}"; do
    echo "=== Voice: $voice ==="
    cat > yap.yaml << EOF
tts:
  adapter: openai
  voice: $voice
EOF
    ./yap --test-tts "$TEXT"
    echo ""
    sleep 1
done

# Restore default
cat > yap.yaml << EOF
tts:
  adapter: openai
  voice: nova
EOF

echo "Done! Pick your favorite and set it in yap.yaml"
