## ADDED Requirements

### Requirement: MySQL's buffer pool is sized for the session views

The quickstart stack SHALL start MySQL with a 1 GB InnoDB buffer pool unless `EDR_MYSQL_BUFFER_POOL_SIZE` in `.env` names another size. The laptop stack SHALL start MySQL with a 512 MB buffer pool under a container memory limit at least 256 MB above it.

#### Scenario: The quickstart buffer pool defaults to 1 GB and follows .env

- **GIVEN** the quickstart compose profile
- **WHEN** MySQL's command line is read
- **THEN** its buffer pool size is `EDR_MYSQL_BUFFER_POOL_SIZE` when set and 1 GB otherwise

#### Scenario: The laptop buffer pool fits under its container limit

- **GIVEN** the laptop compose profile
- **WHEN** MySQL's command line and memory limit are read
- **THEN** its buffer pool is 512 MB and the container limit is at least 256 MB above it
