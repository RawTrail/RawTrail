## 1. Agent

- [x] 1.1 On a contained host, read a decoded provider report without the content filter as the filter stopped, for health, the self-heal and the transition record.

## 2. Tests

- [x] 2.1 `requiredProviders`: contained or not, the filter reported or missing, disabled on purpose.
- [x] 2.2 `observeProviderStatus`: health unhealthy when contained, unchanged when not contained or undecoded; the self-heal re-enables the filter and the stop is recorded.

## 3. Verify

- [x] 3.1 On edr-dev: contain the host, switch the filter's configuration off, and see health report it and the self-heal restore it.
