## 1. Server

- [x] 1.1 The check, its once-a-minute loop beside the detection pipeline, and the alert built for each silent contained host
- [x] 1.2 The response context's contained-host lister, adapted into detection by cmd/main

## 2. Tests

- [x] 2.1 Unit tests for the threshold, the silence clock and the per-containment subject, mutation-checked
- [x] 2.2 An integration test against MySQL for one alert per containment through the real dedup key
- [x] 2.3 Live check on the dev server: contain a host, let it fall silent, see the alert
