#!/bin/zsh
# Stands in for the configuration assistant: writes a helper agent and a belt
# into the library, then takes turns. Every message ends a turn, never the process.
mkdir -p agents/helper belts/tasks
printf '%s\n' '---' 'name: Helper' 'key: helper' 'model: openrouter/stealth/space-bunny-alpha' 'outcomes:' '  - name: done' '    when: Finished' '    tone: good' '    then: next' '---' 'Work on the request.' > agents/helper/v1.md
printf '%s\n' '{"id":"tasks","name":"Tasks","version":1,"trigger":{"kind":"manual"},"repos":["acme/web"],"stages":[{"id":"helper","name":"Helper","type":"agent","model":"openrouter/stealth/space-bunny-alpha","agent":{"id":"helper","version":1},"outcomes":[{"id":"helper-done","name":"done","when":"Finished","tone":"good","then":{"do":"next"}}]}]}' > belts/tasks/v1.json
printf '%s\n' "$PWD" > conversation-directory.txt
echo "conversation ready"
while IFS= read -r message; do
  printf '%s\n' "$message" >> turns.txt
  echo "turn finished: $message"
done
