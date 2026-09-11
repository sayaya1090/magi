# 수명주기 후속 구현 검토 — 2026-09-11

[English](CLIENT_LIFECYCLE_REVIEW_2026-09-11.md) · [목표 설계](CLIENT_LIFECYCLE.ko.md)

검토 기준은 설계 커밋 `2e0179c6` 이후 로컬 `43f45f1c`와 원격 `dcc43ed4`입니다. 둘의 차이는 TUI 계량기 테스트 추가이며 아래 수명주기 코드에는 차이가 없었습니다. 이번 검토는 수명주기·전사 관련 후속 커밋에 집중했습니다. 함께 진행된 회의록·문서 전체를 감사한 결과는 아닙니다. 실행 코드는 수정하지 않았습니다.

## 확인된 문제

구현자 의견도 검토했습니다. R2는 이미 보류된 Windows 인계에 대한 추가 검증 근거입니다. `ownerId`를 소유 모드와 함께 공개한 판단은 타당합니다. 상태 파일이 기존 원칙과 충돌한다는 지적도 수용합니다. `<socket>.lifecycle` 요구는 철회하고, 종료 사유를 확인하지 못하면 미상으로 표시하도록 설계를 수정합니다. 현재 상태는 handshake·`about`으로 판단합니다. 업데이트 복구 저널은 데몬의 현재 생존·소유권 판정에 사용하지 않습니다.

| ID | 우선순위 | 위치·조건 | 영향과 수정 인수 조건 |
|---|---|---|---|
| R1 | P1 | VS Code `src/core/lifecycle.ts`, `await features(binary)` 뒤부터 spawn까지 | 조회 중 `close()`가 끝나도 자식을 생성합니다. 조회 완료 후 세대·closed를 재확인하고 spawn 직전까지 취소를 보장해야 합니다. 생성 이후 close와 경합한 자식도 정리해야 합니다. |
| R2 | P1 | VS Code piped stdin 소유 채널 + Windows `internal/graceful/graceful_windows.go` 후계 기동 | Node는 원래 자식 exit 때 그 stdin을 닫습니다. 후계가 같은 읽기 끝을 상속해도 소유자 EOF를 받으므로 업데이트만으로 소유 데몬이 종료될 수 있습니다. 원래 자식 수명과 독립된 채널을 확보하고 Windows에서 이전 자식 exit와 실제 IDE 종료를 따로 검증해야 합니다. |
| R3 | P1 | JetBrains `StartDaemon.start`: `ProcessBuilder(..., "--daemon")`, 직후 `child.outputStream.close()` | 코어의 소유 모드를 사용하지 않습니다. 정상 dispose의 기존 핸들 종료는 있어도 IDE 강제 종료에 대한 EOF 계약은 적용되지 않습니다. 기능 조회·소유 모드 기동·채널 보관을 함께 연결하고 L05–L07을 실행해야 합니다. |
| R4 | P1 | JetBrains `StartDaemon`의 `Launches` 호출부 | 정책 클래스는 도입했지만 준비 성공 `ready`와 기동 실패 `failed`를 보고하지 않습니다. 처음부터 계속 실패하는 데몬은 연속 실패 횟수가 쌓이지 않아 이동 구간이 지난 뒤 계속 시도할 수 있습니다. 정책 단위 테스트에 더해 실제 기동 실패를 3회 주입하고 이후 시간이 지나도 차단되는지 확인해야 합니다. |
| R5 | P2 | VS Code `OwnedCompanion.launch`의 기능 미지원 분기 | `owned-daemon-v1`이 없으면 일반 `--daemon`으로 새로 시작합니다. 목표 설계 §4의 “필수 소유 모드가 없으면 업데이트 안내와 함께 기동 차단”과 다릅니다. 기존 데몬에 연결하는 호환성과 새 소유 데몬 기동을 구분해야 합니다. |

P1은 종료·복구 인수 전에 해결할 문제, P2는 호환 동작과 설계의 불일치입니다. R3–R5는 구현 경로 확인 결과이며 Windows IDE 실행으로 검증했다는 뜻은 아닙니다.

## 재현과 검증 근거

R1은 현재 TypeScript를 메모리에서 변환하여 기능 조회 Promise만 지연시켰습니다. 순서는 시작 요청→기능 조회 대기→`close()` 완료→기능 응답 해제입니다. 관찰값은 `spawnsAfterClose=1`, `stops=0`이었습니다. 정상 반환된 close 뒤에 자식이 생성되고 정리되지 않는 경합입니다. 기동 전 closed 검사만으로는 막히지 않습니다.

R2는 macOS의 Node v24.4.1에서 프로세스 세 개로 재현했습니다. IDE 역할의 Node가 piped stdin으로 이전 자식을 만들고, 그 자식이 fd 0을 후계에 넘긴 뒤 종료하게 했습니다. IDE 역할의 프로세스를 종료하거나 stdin을 명시적으로 닫지 않았는데 `ownerStillRunning=true`, `stdinDestroyed=true`, `successorSawEOF=true`가 관찰됐습니다. 로컬 Node 런타임의 `internal/child_process`에서도 exit 처리의 `this.stdin.destroy()`를 확인했습니다. Windows 실제 업데이트 검증은 별도로 남아 있습니다.

VS Code 테스트는 232개 중 231개 통과, 1개 건너뜀이었습니다. `go test ./internal/adapter/idebridge ./internal/adapter/daemon ./cmd/magi -run 'Owned|Owner|Instance|Features|Relay' -count=1`도 통과했습니다. 이 결과는 R1/R2의 부재를 보장하지 않습니다. 기존 테스트가 다루는 정상 EOF와 정책 함수에 더해 비동기 종료·세대 교체 경계를 실행해야 합니다.

## 진행된 부분과 다음 인계

기능 조회, 코어의 소유 모드·실행 식별, 두 IDE의 정책 fixture와 백오프 연결, 프리뷰를 최종 사실로 교체하는 구현은 확인했습니다. 이 진전은 유지합니다. 다만 선언·정책 함수·일부 호출부 구현과 설치된 제품의 전체 인수를 구분해야 합니다.

코어·클라이언트 담당자는 R1–R5를 수정하고 각 사례를 재현하는 회귀 테스트를 추가해야 합니다. 자동 업데이트의 안전 시점·프로세스 간 교체 잠금·실제 준비 실패 롤백은 [설계 §9](CLIENT_LIFECYCLE.ko.md#9-자동-업데이트--이번-안정화에-포함)의 추가 인수 범위입니다. 현행 `--version` 사전 검사 롤백만으로 U05를 완료 처리하지 않습니다.
