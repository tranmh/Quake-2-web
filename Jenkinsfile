// The safety net of Quake-2-web, written by aqaa from what its QA run found: unit tests with coverage and its
// floor, integration tests against the databases they need, end-to-end tests, and lint, SAST and dependency
// checks (report only: a finding marks the stage unstable until it is triaged).
// Needs the plugins Pipeline, Docker Pipeline, JUnit, Coverage, Timestamper and Workspace Cleanup;
// integration stages run on an agent labelled 'docker'.
pipeline {
  agent none
  options {
    timeout(time: 90, unit: 'MINUTES')
    timestamps()
    buildDiscarder(logRotator(numToKeepStr: '30', artifactNumToKeepStr: '10'))
    disableConcurrentBuilds(abortPrevious: true)
  }
  environment {
    CI = 'true'
  }
  stages {
    stage('Tests') {
      parallel {
        stage('Unit: c') {
          agent { dockerfile { filename 'c.Dockerfile'; dir 'ci/jenkins' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('.') {
              sh '''
                set -eu
                mkdir -p test-reports
                t=check; for x in check test tests; do if make -n "$x" >/dev/null 2>&1; then t=$x; break; fi; done
                rc=0
                make CC="cc --coverage" "$t" || rc=$?
                gcovr -r .  --exclude '(.+/)?(tests?|unity|cmocka|vendor|third_party|external)/' --gcov-ignore-errors=no_working_dir_found --gcov-ignore-parse-errors=all --cobertura test-reports/coverage.xml
                exit "$rc"
              '''
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'test-reports/*.xml'
              recordCoverage id: 'coverage-c', name: 'Coverage c', tools: [[parser: 'COBERTURA', pattern: 'test-reports/coverage.xml']], sourceDirectories: [[path: '.']]
              archiveArtifacts allowEmptyArchive: true, artifacts: 'test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Unit: go server') {
          agent { docker { image 'golang:1.24' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('server') {
              sh '''
                set -eu
                mkdir -p test-reports
                go mod download
                go run gotest.tools/gotestsum@v1.13.0 --format testname --junitfile test-reports/unit.xml -- -count=1 -covermode=atomic -coverpkg=./... -coverprofile=test-reports/coverage.out ./...
              '''
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'server/test-reports/*.xml'
              recordCoverage id: 'coverage-go-server', name: 'Coverage go server', tools: [[parser: 'GO_COV', pattern: 'server/test-reports/coverage.out']], sourceDirectories: [[path: 'server']], qualityGates: [[threshold: 47.0, metric: 'LINE', baseline: 'PROJECT', criticality: 'FAILURE']]
              archiveArtifacts allowEmptyArchive: true, artifacts: 'server/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Integration: go server') {
          agent { label 'docker' }
          options { timeout(time: 45, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            script {
              docker.image('postgres:16-alpine').withRun('-e POSTGRES_USER=aqaa -e POSTGRES_PASSWORD=aqaa -e POSTGRES_DB=aqaa') { postgres ->
                docker.image('postgres:16-alpine').inside("--link ${postgres.id}:postgres") {
                  sh 'until pg_isready -h postgres -U aqaa -d aqaa; do sleep 1; done'
                }
                docker.image('golang:1.24').inside("--link ${postgres.id}:postgres -e 'AQAA_PG_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_CONNECTION_STRING=Host=postgres;Port=5432;Database=aqaa;Username=aqaa;Password=aqaa' -e 'DATABASE_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_JDBC_URL=jdbc:postgresql://postgres:5432/aqaa?user=aqaa&password=aqaa'") {
                  dir('server') {
                    sh '''
                      set -eu
                      mkdir -p test-reports
                      go mod download
                      RUN_INTEGRATION=1 INTEGRATION=1 go run gotest.tools/gotestsum@v1.13.0 --format testname --junitfile test-reports/integration.xml -- -count=1 -tags integration -run Integration ./...
                    '''
                  }
                }
              }
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'server/test-reports/*.xml'
              archiveArtifacts allowEmptyArchive: true, artifacts: 'server/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('End-to-end: go server') {
          agent { docker { image 'golang:1.24' } }
          options { timeout(time: 45, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('server') {
              sh '''
                set -eu
                mkdir -p test-reports
                go mod download
                RUN_E2E=1 E2E=1 go run gotest.tools/gotestsum@v1.13.0 --format testname --junitfile test-reports/e2e.xml -- -count=1 -tags e2e -run E2E ./...
              '''
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'server/test-reports/*.xml'
              archiveArtifacts allowEmptyArchive: true, artifacts: 'server/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Unit: nextjs web/apps/web') {
          agent { docker { image 'node:22-bookworm' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('web/apps/web') {
              sh '''
                set -eu
                mkdir -p test-reports
                npm install --no-audit --no-fund
                npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
                npx vitest run --coverage --coverage.reporter=cobertura --coverage.reportsDirectory=test-reports/coverage --reporter=default --reporter=junit --outputFile.junit=test-reports/unit.xml --exclude '**/*.integration.test.*' --exclude 'e2e/**'
              '''
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'web/apps/web/test-reports/*.xml'
              recordCoverage id: 'coverage-nextjs-web-apps-web', name: 'Coverage nextjs web/apps/web', tools: [[parser: 'COBERTURA', pattern: 'web/apps/web/test-reports/coverage/cobertura-coverage.xml']], sourceDirectories: [[path: 'web/apps/web']]
              archiveArtifacts allowEmptyArchive: true, artifacts: 'web/apps/web/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('End-to-end: nextjs web/apps/web') {
          agent { docker { image 'mcr.microsoft.com/playwright:v1.56.1-noble' } }
          options { timeout(time: 45, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('web/apps/web') {
              sh '''
                set -eu
                mkdir -p test-reports
                npm install --no-audit --no-fund
                npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
                npx playwright install chromium
                PLAYWRIGHT_JUNIT_OUTPUT_NAME=test-reports/e2e.xml npx playwright test --reporter=junit,list
              '''
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'web/apps/web/test-reports/*.xml'
              archiveArtifacts allowEmptyArchive: true, artifacts: 'web/apps/web/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Unit: sql-postgres server') {
          agent { label 'docker' }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            script {
              docker.build('aqaa-ci-postgres-pgtap', '-f ci/sql/postgres-pgtap.Dockerfile ci/sql').withRun('-e POSTGRES_USER=aqaa -e POSTGRES_PASSWORD=aqaa -e POSTGRES_DB=aqaa') { postgres ->
                docker.image('aqaa-ci-postgres-pgtap').inside("--link ${postgres.id}:postgres") {
                  sh 'until pg_isready -h postgres -U aqaa -d aqaa; do sleep 1; done'
                }
                docker.build('aqaa-ci-sql-postgres', '-f ci/jenkins/sql-postgres.Dockerfile ci/jenkins').inside("--link ${postgres.id}:postgres -e 'AQAA_PG_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_CONNECTION_STRING=Host=postgres;Port=5432;Database=aqaa;Username=aqaa;Password=aqaa' -e 'DATABASE_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_JDBC_URL=jdbc:postgresql://postgres:5432/aqaa?user=aqaa&password=aqaa'") {
                  dir('server') {
                    sh '''
                      set -eu
                      RUNNER_TEMP_DIR="${RUNNER_TEMP:-${WORKSPACE_TMP:-/tmp}}"; export AQAA_TSQLT_DIR="${AQAA_TSQLT_DIR:-$RUNNER_TEMP_DIR/tsqlt}"
                      mkdir -p test-reports
                      if ! command -v squawk >/dev/null || ! python3 -c 'import sqlfluff, pymssql' 2>/dev/null; then python3 -m pip install sqlfluff==4.4.0 squawk-cli==2.67.0 pymssql==2.4.3; fi
                      python3 ci/sql/aqaa-sql test postgres test-reports/unit.xml --level unit
                    '''
                  }
                }
              }
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'server/test-reports/*.xml'
              archiveArtifacts allowEmptyArchive: true, artifacts: 'server/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Integration: sql-postgres server') {
          agent { label 'docker' }
          options { timeout(time: 45, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            script {
              docker.build('aqaa-ci-postgres-pgtap', '-f ci/sql/postgres-pgtap.Dockerfile ci/sql').withRun('-e POSTGRES_USER=aqaa -e POSTGRES_PASSWORD=aqaa -e POSTGRES_DB=aqaa') { postgres ->
                docker.image('aqaa-ci-postgres-pgtap').inside("--link ${postgres.id}:postgres") {
                  sh 'until pg_isready -h postgres -U aqaa -d aqaa; do sleep 1; done'
                }
                docker.build('aqaa-ci-sql-postgres', '-f ci/jenkins/sql-postgres.Dockerfile ci/jenkins').inside("--link ${postgres.id}:postgres -e 'AQAA_PG_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_CONNECTION_STRING=Host=postgres;Port=5432;Database=aqaa;Username=aqaa;Password=aqaa' -e 'DATABASE_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_JDBC_URL=jdbc:postgresql://postgres:5432/aqaa?user=aqaa&password=aqaa'") {
                  dir('server') {
                    sh '''
                      set -eu
                      RUNNER_TEMP_DIR="${RUNNER_TEMP:-${WORKSPACE_TMP:-/tmp}}"; export AQAA_TSQLT_DIR="${AQAA_TSQLT_DIR:-$RUNNER_TEMP_DIR/tsqlt}"
                      mkdir -p test-reports
                      if ! command -v squawk >/dev/null || ! python3 -c 'import sqlfluff, pymssql' 2>/dev/null; then python3 -m pip install sqlfluff==4.4.0 squawk-cli==2.67.0 pymssql==2.4.3; fi
                      python3 ci/sql/aqaa-sql test postgres test-reports/integration.xml --level integration
                    '''
                  }
                }
              }
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'server/test-reports/*.xml'
              archiveArtifacts allowEmptyArchive: true, artifacts: 'server/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Unit: typescript web') {
          agent { docker { image 'node:22-bookworm' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('web') {
              sh '''
                set -eu
                mkdir -p test-reports
                corepack enable
                pnpm install --frozen-lockfile
                npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
                npx vitest run --coverage --coverage.reporter=cobertura --coverage.reportsDirectory=test-reports/coverage --reporter=default --reporter=junit --outputFile.junit=test-reports/unit.xml --exclude '**/*.integration.test.*' --exclude 'e2e/**'
              '''
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'web/test-reports/*.xml'
              recordCoverage id: 'coverage-typescript-web', name: 'Coverage typescript web', tools: [[parser: 'COBERTURA', pattern: 'web/test-reports/coverage/cobertura-coverage.xml']], sourceDirectories: [[path: 'web']]
              archiveArtifacts allowEmptyArchive: true, artifacts: 'web/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Integration: typescript web') {
          agent { label 'docker' }
          options { timeout(time: 45, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            script {
              docker.image('postgres:16-alpine').withRun('-e POSTGRES_USER=aqaa -e POSTGRES_PASSWORD=aqaa -e POSTGRES_DB=aqaa') { postgres ->
                docker.image('postgres:16-alpine').inside("--link ${postgres.id}:postgres") {
                  sh 'until pg_isready -h postgres -U aqaa -d aqaa; do sleep 1; done'
                }
                docker.image('node:22-bookworm').inside("--link ${postgres.id}:postgres -e 'AQAA_PG_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_CONNECTION_STRING=Host=postgres;Port=5432;Database=aqaa;Username=aqaa;Password=aqaa' -e 'DATABASE_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_JDBC_URL=jdbc:postgresql://postgres:5432/aqaa?user=aqaa&password=aqaa'") {
                  dir('web') {
                    sh '''
                      set -eu
                      mkdir -p test-reports
                      corepack enable
                      pnpm install --frozen-lockfile
                      npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
                      npx vitest run integration --reporter=default --reporter=junit --outputFile.junit=test-reports/integration.xml
                    '''
                  }
                }
              }
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'web/test-reports/*.xml'
              archiveArtifacts allowEmptyArchive: true, artifacts: 'web/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
        stage('Unit: typescript web/apps/dev') {
          agent { docker { image 'node:22-bookworm' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('web/apps/dev') {
              sh '''
                set -eu
                mkdir -p test-reports
                npm install --no-audit --no-fund
                npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
                npx vitest run --coverage --coverage.reporter=cobertura --coverage.reportsDirectory=test-reports/coverage --reporter=default --reporter=junit --outputFile.junit=test-reports/unit.xml --exclude '**/*.integration.test.*' --exclude 'e2e/**'
              '''
            }
          }
          post {
            always {
              junit allowEmptyResults: true, testResults: 'web/apps/dev/test-reports/*.xml'
              recordCoverage id: 'coverage-typescript-web-apps-dev', name: 'Coverage typescript web/apps/dev', tools: [[parser: 'COBERTURA', pattern: 'web/apps/dev/test-reports/coverage/cobertura-coverage.xml']], sourceDirectories: [[path: 'web/apps/dev']]
              archiveArtifacts allowEmptyArchive: true, artifacts: 'web/apps/dev/test-reports/**'
            }
            cleanup {
              cleanWs deleteDirs: true, notFailBuild: true
            }
          }
        }
      }
    }
    stage('Checks') {
      parallel {
        stage('Checks: c') {
          agent { dockerfile { filename 'c.Dockerfile'; dir 'ci/jenkins' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('.') {
              sh '''
                set -eu
                mkdir -p test-reports
              '''
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'cppcheck found issues') {
                sh '''
                  set -eu
                  if [ -f build/compile_commands.json ]; then set -- --project=build/compile_commands.json; else set -- .; fi; cppcheck "$@" -ibuild -itests -itest -ivendor -ithird_party --enable=warning,performance,portability --inconclusive --error-exitcode=1 --quiet
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'MISRA C 2012 (cppcheck addon) found issues') {
                sh '''
                  set -eu
                  if [ -f build/compile_commands.json ]; then set -- --project=build/compile_commands.json; else set -- .; fi; cppcheck "$@" -ibuild -itests -itest -ivendor -ithird_party --addon=misra --enable=style --error-exitcode=1 --quiet
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'flawfinder found issues') {
                sh '''
                  set -eu
                  find . \\( -name .git -o -name build -o -name tests -o -name test -o -name vendor -o -name third_party \\) -prune -o \\( -name '*.c' -o -name '*.h' \\) -type f -print0 | xargs -0 -r flawfinder --minlevel=2 --error-level=4 --quiet
                '''
              }
            }
          }
          post {
            always { archiveArtifacts allowEmptyArchive: true, artifacts: 'test-reports/**' }
            cleanup { cleanWs deleteDirs: true, notFailBuild: true }
          }
        }
        stage('Checks: docker') {
          agent { dockerfile { filename 'docker.Dockerfile'; dir 'ci/jenkins' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('.') {
              sh '''
                set -eu
                mkdir -p test-reports
              '''
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'hadolint found issues') {
                sh '''
                  set -eu
                  find . -type f \\( -name Dockerfile -o -name 'Dockerfile.*' -o -name '*.Dockerfile' -o -name Containerfile \\) -not -path './.git/*' -not -path '*/node_modules/*' -print0 | xargs -0 -r hadolint
                '''
              }
            }
          }
          post {
            always { archiveArtifacts allowEmptyArchive: true, artifacts: 'test-reports/**' }
            cleanup { cleanWs deleteDirs: true, notFailBuild: true }
          }
        }
        stage('Checks: go server') {
          agent { docker { image 'golang:1.24' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('server') {
              sh '''
                set -eu
                mkdir -p test-reports
                go mod download
              '''
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'go vet found issues') {
                sh '''
                  set -eu
                  go vet ./...
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'golangci-lint found issues') {
                sh '''
                  set -eu
                  go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --output.text.path stdout --output.sarif.path test-reports/golangci.sarif ./...
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'govulncheck found issues') {
                sh '''
                  set -eu
                  go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
                '''
              }
            }
          }
          post {
            always { archiveArtifacts allowEmptyArchive: true, artifacts: 'server/test-reports/**' }
            cleanup { cleanWs deleteDirs: true, notFailBuild: true }
          }
        }
        stage('Checks: nextjs web/apps/web') {
          agent { docker { image 'node:22-bookworm' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('web/apps/web') {
              sh '''
                set -eu
                mkdir -p test-reports
                npm install --no-audit --no-fund
                npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
              '''
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'eslint found issues') {
                sh '''
                  set -eu
                  if ls eslint.config.* .eslintrc* >/dev/null 2>&1; then npx eslint .; else echo 'no ESLint configuration'; fi
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'tsc found issues') {
                sh '''
                  set -eu
                  npx tsc --noEmit
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'npm audit found issues') {
                sh '''
                  set -eu
                  npm audit --audit-level=high --omit=dev
                '''
              }
            }
          }
          post {
            always { archiveArtifacts allowEmptyArchive: true, artifacts: 'web/apps/web/test-reports/**' }
            cleanup { cleanWs deleteDirs: true, notFailBuild: true }
          }
        }
        stage('Checks: sql-postgres server') {
          agent { label 'docker' }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            script {
              docker.build('aqaa-ci-postgres-pgtap', '-f ci/sql/postgres-pgtap.Dockerfile ci/sql').withRun('-e POSTGRES_USER=aqaa -e POSTGRES_PASSWORD=aqaa -e POSTGRES_DB=aqaa') { postgres ->
                docker.image('aqaa-ci-postgres-pgtap').inside("--link ${postgres.id}:postgres") {
                  sh 'until pg_isready -h postgres -U aqaa -d aqaa; do sleep 1; done'
                }
                docker.build('aqaa-ci-sql-postgres', '-f ci/jenkins/sql-postgres.Dockerfile ci/jenkins').inside("--link ${postgres.id}:postgres -e 'AQAA_PG_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_CONNECTION_STRING=Host=postgres;Port=5432;Database=aqaa;Username=aqaa;Password=aqaa' -e 'DATABASE_URL=postgres://aqaa:aqaa@postgres:5432/aqaa?sslmode=disable' -e 'AQAA_PG_JDBC_URL=jdbc:postgresql://postgres:5432/aqaa?user=aqaa&password=aqaa'") {
                  dir('server') {
                    sh '''
                      set -eu
                      RUNNER_TEMP_DIR="${RUNNER_TEMP:-${WORKSPACE_TMP:-/tmp}}"; export AQAA_TSQLT_DIR="${AQAA_TSQLT_DIR:-$RUNNER_TEMP_DIR/tsqlt}"
                      mkdir -p test-reports
                      if ! command -v squawk >/dev/null || ! python3 -c 'import sqlfluff, pymssql' 2>/dev/null; then python3 -m pip install sqlfluff==4.4.0 squawk-cli==2.67.0 pymssql==2.4.3; fi
                    '''
                    catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'migration check found issues') {
                      sh '''
                        set -eu
                        RUNNER_TEMP_DIR="${RUNNER_TEMP:-${WORKSPACE_TMP:-/tmp}}"; export AQAA_TSQLT_DIR="${AQAA_TSQLT_DIR:-$RUNNER_TEMP_DIR/tsqlt}"
                        python3 ci/sql/aqaa-sql migrate postgres test-reports/migrations.json --fail
                      '''
                    }
                    catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'squawk found issues') {
                      sh '''
                        set -eu
                        RUNNER_TEMP_DIR="${RUNNER_TEMP:-${WORKSPACE_TMP:-/tmp}}"; export AQAA_TSQLT_DIR="${AQAA_TSQLT_DIR:-$RUNNER_TEMP_DIR/tsqlt}"
                        python3 ci/sql/aqaa-sql squawk test-reports/squawk.json --fail
                      '''
                    }
                    catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'sqlfluff found issues') {
                      sh '''
                        set -eu
                        RUNNER_TEMP_DIR="${RUNNER_TEMP:-${WORKSPACE_TMP:-/tmp}}"; export AQAA_TSQLT_DIR="${AQAA_TSQLT_DIR:-$RUNNER_TEMP_DIR/tsqlt}"
                        python3 ci/sql/aqaa-sql sqlfluff postgres test-reports/sqlfluff.json --fail
                      '''
                    }
                  }
                }
              }
            }
          }
          post {
            always { archiveArtifacts allowEmptyArchive: true, artifacts: 'server/test-reports/**' }
            cleanup { cleanWs deleteDirs: true, notFailBuild: true }
          }
        }
        stage('Checks: typescript web') {
          agent { docker { image 'node:22-bookworm' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('web') {
              sh '''
                set -eu
                mkdir -p test-reports
                corepack enable
                pnpm install --frozen-lockfile
                npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
              '''
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'eslint found issues') {
                sh '''
                  set -eu
                  if ls eslint.config.* .eslintrc* >/dev/null 2>&1; then npx eslint .; else echo 'no ESLint configuration'; fi
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'tsc found issues') {
                sh '''
                  set -eu
                  npx tsc --noEmit
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'npm audit found issues') {
                sh '''
                  set -eu
                  npm audit --audit-level=high --omit=dev
                '''
              }
            }
          }
          post {
            always { archiveArtifacts allowEmptyArchive: true, artifacts: 'web/test-reports/**' }
            cleanup { cleanWs deleteDirs: true, notFailBuild: true }
          }
        }
        stage('Checks: typescript web/apps/dev') {
          agent { docker { image 'node:22-bookworm' } }
          options { timeout(time: 30, unit: 'MINUTES') }
          steps {
            checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs, extensions: scm.extensions + [[$class: 'SubmoduleOption', recursiveSubmodules: true, parentCredentials: true]]])
            dir('web/apps/dev') {
              sh '''
                set -eu
                mkdir -p test-reports
                npm install --no-audit --no-fund
                npm install --no-save --no-audit --no-fund "@vitest/coverage-v8@$(node -p "require('vitest/package.json').version")"
              '''
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'eslint found issues') {
                sh '''
                  set -eu
                  if ls eslint.config.* .eslintrc* >/dev/null 2>&1; then npx eslint .; else echo 'no ESLint configuration'; fi
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'tsc found issues') {
                sh '''
                  set -eu
                  npx tsc --noEmit
                '''
              }
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', message: 'npm audit found issues') {
                sh '''
                  set -eu
                  npm audit --audit-level=high --omit=dev
                '''
              }
            }
          }
          post {
            always { archiveArtifacts allowEmptyArchive: true, artifacts: 'web/apps/dev/test-reports/**' }
            cleanup { cleanWs deleteDirs: true, notFailBuild: true }
          }
        }
      }
    }
  }
}
