#!/usr/bin/env bash
export PROVIDER="aistudio"
export AI_STUDIO_API_KEY="your api key"

git clone https://github.com/gosuda/website
cd website && make build

cd .. && cp ./website/dist dist && rm --rf website