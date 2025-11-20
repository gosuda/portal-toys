# Legacy `mafia/reference.js` Breakdown

이 문서는 기존 JavaScript 구현(`golang/mafia/reference.js`)의 구조와 규칙을 분석해 정리한 것입니다. Portal SDK 기반 Go 서버를 재작성할 때 참고하도록 전체 상태·흐름·직업 메커니즘을 Markdown 형태로 문서화했습니다.

## 1. 전역 구조

| 식별자 | 설명 |
| --- | --- |
| `mafiaList` | 생성된 방 이름 목록. `makeRoom` 시 push, 방 삭제 시 제거. |
| `mafia` | `roomName -> RoomState` 매핑. 각 방의 모든 게임 상태가 이 객체에 저장. |
| `mafiaJoin` | `playerName -> roomName` 매핑. 플레이어가 어느 방에 속해 있는지 추적. |
| `mafiaFn` | 게임 전반을 제어하는 함수 컬렉션. (채팅 브로드캐스트, 프리픽스 관리, 방 생성/참여/퇴장, 투표 진행 등) |
| `module` helpers | `module.rA`(array remove), `module.randomArr`, `newObject`, `reply`, `setTimeout` 등 실행 환경에서 제공하는 유틸 함수. |

## 2. RoomState 필드

`mafia[room]` 객체는 아래와 같은 필드를 가집니다.

- `game` / `isDay` / `isVote`: 현재 게임 진행 여부, 낮/밤, 투표 단계 여부.
- `specialJob`: 기본 역할 템플릿. `시민`(citizen), `악인`(mafia) 정의.
- `job`: 실제 직업 사전. 각 항목에 `type`, `desc`, `impor`, `onChat`, `onSelect`, `onDeath` 등 콜백 포함.
- `onDay`, `onSelect`, `onVote`: 전역 훅. 예) `onDay`는 밤 행동 해석, 도굴꾼 처리, 짐승인간/마피아/의사/탐정 결과를 브로드캐스트.
- `trick`, `trickList`, `lockSel`: 마술사 트릭 대상 및 swap 상태 추적.
- `prefix`: 플레이어별 메모(닉네임 프리픽스). `setPrefix`로 갱신.
- `playerJob` / `ogPlayerJob`: 현 직업 및 원래 직업.
- `cantUse`: 마담/건달 등의 제한으로 능력 사용 불가인 플레이어.
- `select`: 직업별 선택 결과 저장(예: `select.의사[name] = target`).
- `selectName`: 선택 내역 표시용.
- `vote`, `voteList`, `cantVote`: 투표 카운트, 투표한 플레이어 목록, 건달 등으로 투표 불가 대상.
- `isExecution`, `isExecuList`, `deathTarget`: 처형 단계 관리용.
- `editTime`: `reductTime`/`addTime`로 시간 조절한 플레이어 기록.
- `deathList`: 사망자 목록.
- `cantChat`: 영매가 성불시킨 플레이어 등 채팅 불가 대상.
- `list`: 방에 속한 플레이어 순서. `sayList`에서 사용.
- `chains`: 밤/낮 사이클 카운터.
- `eventTimer`: 낮 토론 타이머. `reductTime`/`addTime`과 투표 전 예고에 사용.

## 3. 주요 함수 요약

### `mafiaFn.say(room, msg)`
- 방 내 전체 플레이어에게 메시지 브로드캐스트.

### `mafiaFn.chat(sender, msg)`
- 현재 단계/직업 상태에 따라 메시지의 수신 범위를 결정.
  - 게임 전: 전원에게 전달.
  - 낮: 전체 공개하지만 사망자/마술사 트릭 등 조건에 따라 별도 경로.
  - 밤: 직업별 전용 채널 (마피아 접선, 영매/연인 등).
  - 사망자: 사망자끼리/영매에게만 공유.

### `mafiaFn.getPrefix / setPrefix`
- 각 플레이어가 다른 플레이어에게 부여한 메모(식별자)를 관리.

### 방/플레이어 관리
- `makeRoom(roomName)`: 새 RoomState 초기화 + 기본 필드 세팅.
- `join(sender, roomIndex)`: 기존 방에 합류. 게임 중이면 거부.
- `quit(sender)`: 방에서 퇴장 (게임 중이면 거부). 방장이 나가면 방 삭제.
- `kick(sender, num)`: 방장의 강퇴 기능.

### 게임 진행
- `start`: 방장이 커맨드로 게임 시작 → 역할 배정(`assignRoles`) → 밤 시작.
- `nextDay`: 밤 종료 → 낮 시작 → `sayList`/`sayAbi` 브로드캐스트 → 타이머 세팅.
- `proceed`: 낮 종료 → 투표 시간(`vote`) 시작.
- `vote`: 플레이어 번호 입력으로 투표. 타이머 종료 후 `totalVote` 실행.
- `totalVote`: 최다득표자 선정 → 최후 변론/찬반 투표(`agreeOpo`).
- `agreeOpo`: 찬성/반대 카운트 → `execAgree`/`execOppo`로 응답 → 처형 여부 결정.
- `checkGameOver`: 생존 팀 수에 따라 승리 조건 평가 (마피아 승, 시민 승, 교주 승 등).
- `gameOver`: 결과 브로드캐스트 후 RoomState 초기화.

### 선택/능력 관련 헬퍼
- `select` 맵 + `selectName`: 밤 능력 타깃 저장.
- `cantUse`, `cantVote`, `cantChat`: 직업 효과로 행동 제한을 받은 플레이어 목록.
- `trick`, `trickList`, `lockSel`: 마술사의 몸 바꾸기 로직.
- `deathTarget`: 찬반 투표 대상 + 찬성/반대 수 + 발언 가능 여부.
- `eventTimer`, `reductTime`, `addTime`: 낮 타이머 조절.

## 4. 타이머/시간 흐름

| 단계 | 기본 시간 | 설명 |
| --- | --- | --- |
| 밤 (`nextDay` 호출 후) | 25초 → 10초 경고 | 밤 행동 입력, `setTimeout("mafia Night Timer"+room, …)` 구조. |
| 낮 토론 | `playersAlive * 15`초 | 토론 시간, 30초/10초 남았을 때 알림. |
| 투표 입력 | 10초 | `vote()` 호출 후 타이머. |
| 최후 변론 | 10초 | `deathTarget.say` 기간. |
| 찬반 투표 | 10초 | `agreeOpo` 실행. |

플레이어는 `reductTime`/`addTime` 명령으로 낮 토론 시간을 ±15초 조절(1인 1회).

## 5. 커뮤니케이션 규칙

| 상황 | 처리 |
| --- | --- |
| 생존 낮 채팅 | 전체 공개. `prefix`로 메모 표시. |
| 생존 밤 채팅 | 기본적으로 불가. 마피아/접선한 직업/연인/영매 등은 전용 채널 허용. |
| 사망자 채팅 | 사망자끼리 공유, 영매는 사망자 메시지를 실시간 수신. |
| 마술사 트릭 | `trick`에 매핑되어 있으면 이름이 바뀌거나 대신 죽음. |

## 6. 투표/처형 흐름

1. 낮 종료 → `vote()`에서 표 수집.
2. `totalVote()`가 최다득표자를 `deathTarget`에 설정. 동률이면 무효 → 밤으로.
3. 최후 변론 10초 후 `agreeOpo()` 시작.
4. 각 플레이어는 `execAgree`/`execOppo`로 찬반 입력. `isExecuList`로 중복 방지.
5. 찬성 ≥ 반대 → 처형. 일부 직업(정치인, 테러리스트 등)은 `onVoteDeath`로 특수 처리.

## 7. 상태 맵 정리

| 맵/리스트 | 용도 |
| --- | --- |
| `trick`, `trickList`, `lockSel` | 마술사의 몸 바꾸기 조합 관리. |
| `select` | 직업별 타깃 저장 (`select.의사[name] = player`). |
| `selectName` | 프롬프트용 표시. |
| `cantUse` | 건달·마담 등에 의해 능력 사용 불가인 플레이어. |
| `cantVote` | 투표 불가 플레이어. |
| `cantChat` | 영매 성불 등으로 채팅 금지된 플레이어. |
| `deathList` | 사망자. 일부 직업(영매, 성직자)이 참조. |
| `prefix` | 플레이어별 메모. `getPrefix`/`setPrefix`. |
| `vote`, `voteList`, `deathTarget` | 투표 집계 및 처형 단계 상태. |

## 8. 직업 목록

`job` 객체에 정의된 21개 직업과 주요 속성(유형, 능력 설명, 설정값)은 다음과 같습니다.

| 직업 | 팀 | 설명 | 주요 속성 |
| --- | --- | --- | --- |
| 시민 | citizen | 아무 능력이 없습니다. | `canSelect: false` |
| 악인 | mafia | 아무 능력이 없습니다. | `canSelect: false` |
| 마피아 | mafia | 밤마다 플레이어 한 명을 죽일 수 있다. | `impor: true` (필수 직업) + 접선 시 마피아 간 비공개 채팅/타깃 공유 |
| 경찰 | citizen | 밤마다 한 사람을 조사해 마피아 여부 확인. | `impor: true`, `canSelect: 2`(번호 선택 입력) |
| 의사 | citizen | 밤마다 한 사람을 보호. | `impor: true`, `canSelect: true` |
| 군인 | citizen | 마피아 공격 1회 버팀. | `canSelect: false`; `onDeath` 훅을 통해 첫 공격 무효화 |
| 정치인 | citizen | 투표로 처형되지 않으며 투표권 2표. | `canSelect: false`, `voteCount: 2`; `onVoteDeath`에서 사망 무효 |
| 영매 | citizen | 사망자 채팅 수신 + 죽은 사람 직업 확인 후 성불. | `canSelect: 2`, `canDeadSelect: true` (죽은 대상 지정 가능) |
| 연인 | citizen | 밤에 연인끼리 대화, 한 명이 지명되면 다른 연인이 대신 죽음. | `canSelect: 2`, `count: 2` (직업 수량 두 명) |
| 기자 | citizen | (첫날 제외) 밤에 1명 취재 후 다음 날 직업 공개 (1회용). | `canSelect: true`; 사용 후 `canSelect=false` 처리 |
| 건달 | citizen | 밤에 협박한 플레이어는 다음날 모든 투표 불가. | `canSelect: 2` |
| 도굴꾼 | citizen | 첫날 마피아에게 죽은 사람의 직업을 훔침. | `canSelect: false`; 밤 해석 시 직업 교체 |
| 사립탐정 | citizen | 밤에 조사 대상이 누구를 선택했는지 확인. | `canSelect: 2` |
| 테러리스트 | citizen | 자폭 대상 지정. 마피아에게 죽으면 지정한 마피아와 동반 자폭. 투표로 죽을 때 반론에서 대상 지정 가능. | `canSelect: true`; `onDeath`, `onVoteDeath` 특수 처리 |
| 성직자 | citizen | 죽은 플레이어를 부활시키며 교주 포교 면역. | `canSelect: true`, `canDeadSelect: true` (1회용) |
| 마술사 | citizen | 1회용 트릭: 밤에 바꿔치기 대상 지정 → 자신이 죽으면 대상이 대신 사망. | `canSelect: 2`; `lockSel`, `trickList` 사용 |
| 스파이 | citizen → 마피아 접선 가능 | 밤마다 직업 탐지, 마피아를 조사하면 접선. | `canSelect: 2`, `subMafia: true`, `contact: false` (접선 후 true), `condi: 6`, `impor: true` |
| 마담 | citizen → 마피아 접선 가능 | 낮 투표 대상 유혹 → 능력 봉인, 마피아 대상이면 접선. | `canSelect: 3`, `subMafia: true`, `contact: false`, `condi: 6`, `impor: true` |
| 도둑 | citizen → 마피아 접선 가능 | 투표한 플레이어의 능력을 훔쳐 잠시 사용. 마피아 직업 훔치면 접선. | `canSelect: 3`, `subMafia: true`, `contact: false`, `condi: 6`, `impor: true` |
| 짐승인간 | citizen → 마피아 접선 가능 | 자신이 선택한 대상이 마피아 타깃이면 접선, 대상 처형 방해 무시. | `canSelect: true`, `subMafia: true`, `contact: false`, `condi: 6`, `impor: true` |
| 교주 | sect (포교 팀) | 홀수 번째 밤마다 포교 대상 지정, 직업 확인, 일방 소통 가능. 성직자/마피아는 포교 불가. | `canSelect: 2`, `condi: 9`, `impor: true` |

> **특수 직업**: `시민/악인`은 도굴꾼, 포교, 직업 박탈 시 대체 역할로 사용됩니다.

## 9. 흐름 요약 도표

```
makeRoom → join/quit → (host) start
   ↓ (Night)
 select abilities (마피아/의사/경찰/특수) → onDay 해석
   ↓ (Day)
 sayList + 토론 타이머 (reduct/add time 가능)
   ↓
 vote() → totalVote → 최후 변론 → agree/oppose
   ↓
 eliminate target / special hooks
   ↓
 checkGameOver ? → gameOver : nextDay
```

## 10. 직업별 세부 로직

아래 표는 `job` 객체에 정의된 모든 직업의 이벤트 훅을 정리한 것입니다. 각 직업은 `mafiaFn` 내부 로직과 다양한 상태 맵을 조작하므로, Go 재구현 시 같은 흐름을 재현해야 합니다.

| 직업 | onChat | onSelect | onDeath / onAnyDeath | 기타 훅 및 부가 설명 |
| --- | --- | --- | --- | --- |
| **마피아** | 밤에 접선된 마피아/스파이/마담/도둑/짐승인간에게만 메시지 전달. 사망자에게는 `[마피아 팀]` 태그로 공유. | `select.ms`에 타깃 저장, 마피아 팀(및 짐승인간)에게 선택 결과 브로드캐스트. | 일반 사망 시 `deathList`에 추가. | `impor: true`. 짐승인간과 접선시 `contact=true`. |
| **경찰** | (별도 onChat 없음) | `canSelect: 2`. 선택 시 조사 결과를 본인에게만 알려 줌. | - | 조사 결과가 마피아면 “마피아입니다” 메시지, 아니면 “아닙니다”. |
| **의사** | - | `canSelect: true`. `select.의사[name] = target`. | - | `onDay`에서 마피아 타깃 == 의사 타깃이면 살해 무효 메시지. |
| **군인** | - | - | `onDeath`: 첫 공격 버팀(`respawn` 플래그), 두 번째에 사망하고 `deathList` 추가. | 트리거 시 방 전체에 메시지. |
| **정치인** | - | - | `onVoteDeath`: 투표 처형 면역, 직업 공개 후 메시지. | `voteCount = 2`. |
| **영매** | 사망자 채팅/밤 대화 수신, 밤에 사망자들과 양방향 대화. | `canSelect: 2`, `canDeadSelect: true`. 선택한 사망자 직업을 본인에게 알리고 대상 `cantChat`으로 성불 처리. | - | 밤에 사망자 채팅을 실시간 중계. |
| **연인** | 밤에 살아있는 연인끼리 개인 채팅, 사망자에게는 `[연인]` 태그. | `count: 2`. 별도 onSelect 없음 (배정 시 두 명). | `onDeath`: 연인 둘다 생존 중이면 다른 연인이 대신 사망(“감싸고 살해”). | - |
| **기자** | - | `canSelect: true`. 첫날 밤(`chains <= 1`)에는 사용 불가. 사용 시 다음 날 모두에게 직업 공개, 본인 능력 비활성화. | - | 한 번 사용 후 `canSelect=false`. |
| **건달** | - | `canSelect: 2`. 선택한 플레이어는 다음 투표에 참여 불가(`cantVote`). | - | 대상에게 협박 메시지 전송. |
| **도굴꾼** | - | - | `onDay`에서 첫날 마피아에게 살해당한 사람의 직업을 도굴 → 본인 직업 교체, 대상은 시민/악인으로 강등. | 조건: `mafia[room].chains == 1`. |
| **사립탐정** | - | `canSelect: 2`. 대상이 능력을 사용했다면 `selectName`을 확인해 대상자 이름을 알려 준다. | - | `mafiaFn.onSelect`에서 탐정 대상에게 “지목” 메시지 전달. |
| **테러리스트** | - | `canSelect: true`. `select[직업][name] = target`. | `onDeath`: 지정 대상이 마피아이면 자폭하며 둘 다 죽음. `onVoteDeath`: 반론에서 선택해 같이 처형 가능. | 투표로 죽을 때 `deathTarget.name`과 대상 비교. |
| **성직자** | - | `canSelect: true`, `canDeadSelect: true`. 선택 사망자를 부활(`deathList`에서 제거). | - | 부활 실패 시(성불 대상 등) “부활 실패” 메시지, 능력 소모. 교주 포교 면역. |
| **마술사** | - | `canSelect: 2`. 대상에게 트릭 설정(`lockSel`). 사용 후 능력 비활성. | `onAnyDeath`: 마술사가 죽으면 설정한 대상이 대신 죽음, `trick`/`trickList` 갱신. | 트릭 카운터/팀 교체(`trickTeam`). |
| **스파이** | 마피아 팀과 접선하면(마피아 대상 조사 성공) 마피아 채널에 참여. | `canSelect: 2`. 마피아 조사 성공 시 `contact=true`, 마피아와 상호 접선 메시지. 군인 조사 시 대상에게 통지. | - | `subMafia: true`, `impor: true`, `condi: 6`. |
| **마담** | 마피아와 접선 시 마피아 채널에 참여. | `canSelect: 3`. 투표 대상에게 유혹 메시지, 마피아 대상이면 접선(`contact=true`), 시민 대상은 `cantUse`로 능력 봉인. | `onAnyDeath`: 마담 사망 시 `cantUse` 목록 초기화. | `subMafia: true`, `condi: 6`. |
| **도둑** | 마피아 직업을 훔치면 접선(마피아 채팅 참여). | `canSelect: 3`. 투표 대상으로부터 직업 복사. 군인/교주 등 특수 직업 훔치기 실패 조건 처리. | - | `subMafia: true`, `condi: 6`. |
| **짐승인간** | 접선 시 마피아 채널 참여. | `canSelect: true`. 밤 타깃(`select.ms`) 저장. 마피아 타깃과 일치하면 접선, 대상 사망 처리. | - | `subMafia: true`, `condi: 6`. |
| **교주** | 포교된 플레이어에게 일방 메시지 전달(`mafiaFn.say`). | `canSelect: 2`. 홀수 밤(`condi: 9`)마다 포교 대상 지정, 포교 성공 시 대상 `type = sect`. 성직자/마피아는 포교 불가. | 포교 대상 사망 시 별도 처리 없음. | 포교 시 피해자에게 “종소리” 메시지, 교주는 대상 직업을 알 수 있음. |

각 직업의 상세 콜백은 `job[직업명].onChat / onSelect / onDeath / onDay / onVoteDeath / onAnyDeath` 등에 정의되어 있으며, 고유 상태(`trickList`, `cantVote`, `select`, `cantUse`, `playerJob[*].contact`)를 수정합니다. Go 재구현 시 동일 훅 체계를 구성하고 직업별 로직을 함수형으로 분리해야 합니다.

## 11. 구현 시 고려사항

- **타이머**: JS 버전은 문자열 ID 기반 `setTimeout`/`setInterval`. Go에서는 `time.Timer`/`time.AfterFunc`로 대체해야 하며, 각 타이머 콜백에서 룸 이벤트 루프로 안전하게 enqueue.
- **동시성**: JS는 단일 스레드지만 Go는 병렬 실행되므로 룸별 mutex 또는 상태 머신(현재 구현처럼 채널 기반) 필요.
- **직업 훅**: JS는 `eval(job.onChat)` 등 동적 실행. Go에서는 인터페이스/함수 포인터 혹은 스크립트 엔진 필요. 복잡한 콜백을 지원하려면 `Job` 인터페이스와 컨텍스트 객체를 설계해야 함.
- **상태 맵**: `prefix`, `trick`, `select`, `cantUse`, `cantChat` 등이 각 직업과 연결되어 있으므로, 그대로 보존하거나 역할별 구조체로 재구성해야 reference UX를 재현 가능.

이 문서(특히 직업별 표)를 기반으로 각 콜백의 세부 로직을 점진적으로 코드에 이식하거나, 더 상세한 사양서(예: 직업별 단계별 시나리오)를 작성할 수 있습니다. 추가 직업/이벤트가 필요하면 해당 섹션을 확장하십시오.

## 12. `module` / 런타임 헬퍼 사양

`reference.js`는 런타임에 내장된 헬퍼와 문자열 기반 타이머에 크게 의존합니다. Go 이식 시 동일한 동작을 제공해야 합니다.

| 함수/객체 | 역할 | JS 구현 맥락 |
| --- | --- | --- |
| `reply(target, msg)` | 특정 플레이어에게 DM 전송. `mafiaFn.chat`, 직업 onChat, onSelect 곳곳에서 사용. | Portal SDK 버전에선 각 WebSocket 세션으로 변환 필요. |
| `module.rA(array, value)` | 배열에서 value 제거(remove Array). 플레이어 퇴장, 직업 큐 소모 등에 사용. | Go에선 `slices.DeleteFunc` 또는 hand-rolled filter로 대신. |
| `module.randomArr(array)` | 배열에서 무작위 원소 pop. 직업/플레이어 배정에 사용. | Go `rng.Intn(len)` 패턴. |
| `newObject(obj)` | 얕은 복사. 직업 템플릿 복제 시 사용. | Go에서는 구조체 복사/Deep copy 필요. |
| `setTimeout(id, fn, delay)` / `clearTimeout(id)` | 문자열 키 기반 타이머. `"mafia Night Timer"+room` 식으로 식별자를 조립. | Go는 `time.Timer`를 룸 상태에 보관하고 중복 실행 방지. |
| `setInterval(id, fn, delay)` / `clearInterval(id)` | 낮 토론 타이머(`mafia Vote Timer`). 매초 eventTimer 감소. | Go는 `time.Ticker` or `time.AfterFunc` 루프. |

## 13. `mafiaFn` API 상세 목록

| 함수 | 설명 및 상태 변경 |
| --- | --- |
| `say(room, msg)` | 방 내 모든 플레이어에게 메시지. 내부적으로 `reply` 반복 호출. 사망자/영매 등 예외 없음. |
| `chat(sender, msg)` | 가장 복잡한 함수. (1) 플레이어 룸 확인 → (2) 게임 진행 여부 확인 → (3) 단계/직업별 분기. 밤에는 `cantChat`, 마술사 트릭(`trick`, `trickList`), 사망자 (`deathList`), 영매(`playerJob[*].name == "영매"`), 접선 여부(`contact`)에 따라 대상 결정. |
| `getPrefix(sender, target, room)` | 게임 중이면 동일 직업 여부 검사 → 그렇지 않으면 `prefix[sender][target]` 사용, 없으면 "메모 없음". |
| `setPrefix(sender, idx, value, room, subject)` | subject==true 면 전체 플레이어에게 동일 메모 부여(예: 직업 공개). 아니라면 `sender`의 prefix만 갱신. |
| `makeRoom(room)` | 중복 방 여부 체크 → `mafiaList` push → RoomState 초기화. `specialJob`, `job`, `trick`, `select`, `vote` 등 모든 필드를 기본값으로 설정하고, 방장/참여자도 `room` 이름으로 초기화. |
| `join(sender, roomIndex)` | `mafiaJoin` 검사 → 게임 중인지 확인 → `list`에 플레이어 추가 후 입장 메시지/인원 수 브로드캐스트. |
| `quit(sender)` | 게임 중이면 거부. 방장 퇴장 시 방 삭제(`mafiaList`, `mafiaJoin` 정리). 일반 플레이어면 `module.rA`로 리스트 제거 후 `sayList`. |
| `kick(sender, num)` | 방장만 사용. 대상 인덱스 범위 체크 후 강제 퇴장, `mafiaJoin`에서 제거. |
| `sayList(room)` | 각 플레이어별로 현재 인원 목록과 prefix/생존 여부를 DM. |
| `sayAbi(room)` | 생존자에게 자신의 직업/능력 설명을 DM. `deathList`와 `trick` 상태를 고려. |
| `sayJob(room)` | 게임 종료 시 직업 공개. `ogPlayerJob` 기준. |
| `start(room)` | 인원수/상태 검증 후 `assignRoles` 호출. |
| `assignRoles(room)` | `job` 사전 기반으로 필수 직업(`impor`), subMafia 직업, 일반 직업을 섞어 플레이어에게 할당. `module.randomArr`와 `newObject` 사용. |
| `nextDay(room)` | 밤↔낮 토글, `chains++`, 타이머 안내 메시지, `sayList`/`sayAbi`, 25초/10초 타이머 예약. |
| `onDay(room)` | 밤 행동 해석의 핵심. `select`맵을 순회하며 마피아 타깃, 의사 보호, 경찰 조사, 짐승인간, 도굴꾼 등을 처리. 마지막에 `select`, `selectName` 초기화. |
| `proceed(room)` | 낮 → 투표 전환. `eventTimer` 초기화 후 1초간 반복 감소, 30/10초 안내, 0초 시 `vote()`. |
| `vote(room)` | `isVote = true`, 플레이어에게 투표 안내, 10초 후 최후 변론 예정. |
| `totalVote(room)` | `vote` 맵에서 최다 득표자의 이름을 찾고 동률 처리. 대상 설정 후 최후 변론/찬반 단계 진입. |
| `deathTarget` 구조 | `{name, agree, opposition, say}`. `say`가 true인 동안만 발언 가능. |
| `agreeOpo(room)` | 찬반 투표 시작. `execAgree/execOppo`를 통해 입력을 받고, 타이머 종료 시 남은 플레이어 수만큼 자동으로 반대 표 추가. |
| `execAgree/execOppo(sender)` | 마술사 트릭, `cantVote`, `isExecuList` 등을 검사한 뒤 `deathTarget` 카운트 증가. |
| `reductTime/addTime(sender)` | 낮 토론 시간 ±15초 조절. 각 플레이어 1회 제한(`editTime`). |
| `getCanVote(room)` | 팀별 생존자 수 계산. `playerJob[*].trickTeam`(마술사에 의해 팀이 바뀐 경우) 고려. |
| `checkGameOver(room)` | `getCanVote` 결과를 기반으로 시민/마피아/교주 승 조건 평가. |
| `gameOver(room)` | 승리 메시지 → `sayJob` → RoomState 초기화 (`playerJob`, `deathList`, `vote`, `select`, `trick` 등 모두 초기 상태로). |

## 14. 상태/타이머 상호작용 상세

1. **밤 단계**
   - 타겟 저장: `select.ms`(마피아), `select.의사`, `select.경찰`, `select.연인`, `select.건달` 등.
   - 트릭: 마술사가 밤에 `lockSel[name] = target`. 사망 시 `trick[name] = lockSel[name]`, 대상의 `playerJob` 팀을 트릭 팀으로 덮어씀.
   - 포교: 교주는 홀수 밤(`chains` 기준)에서만 `select.교주[name] = target` 가능. `condi` 값이 조건을 제어.
2. **`onDay` 평가 순서** (요약)
   1. 의사 보호 목록(`select.의사`)과 마피아 타깃 비교.
   2. 짐승인간: `select.ms`가 짐승인간을 가리키는지 확인 → 접선 및 즉사 처리.
   3. 마피아 공격 처리 및 마술사 `onAnyDeath` 호출.
   4. 도굴꾼: 첫 밤 사망자의 직업을 도굴.
   5. 기자/사립탐정: `select`에 남아 있는 조사 결과를 브로드캐스트.
   6. `select`, `selectName` 초기화.
3. **낮 토론 타이머**
   - `eventTimer = aliveCount * 15`. `setInterval`로 1초마다 감소, 30초/10초 안내.
   - 플레이어가 `reductTime`/`addTime`을 호출하면 ±15초 조정. `editTime`에 기록해 중복 호출 방지.
4. **투표/찬반 타이머**
   - 투표 입력 10초(`setTimeout`). → 최후 변론 10초 → 찬반 투표 10초.
   - 찬반 단계에서는 아직 표를 던지지 않은 생존자 수만큼 자동 반대표 계산.
5. **샤딩/정리**
   - 방에 아무도 남지 않으면 `mafiaFn.removeRoom` (Go 버전에서는 `RoomManager.removeRoom`).
   - 게임 종료 후 모든 상태 맵 초기화, `trick`/`select`/`vote`/`deathList` 클리어.

---

위 확장 문서는 reference.js 전역 함수·헬퍼·타이머·직업 이벤트를 모두 구조적으로 정리하기 위한 기반입니다. 각 직업의 콜백을 실제 코드로 옮길 때 이 표와 순서를 참고해 동일한 상태 변화를 구현해야 합니다.

## 15. 직업별 이벤트 시나리오

아래 표는 주요 직업의 이벤트 흐름을 단계별로 상세히 설명합니다. `S=`는 상태맵, `EV=`는 브로드캐스트 이벤트, `DM=`은 DM(`reply`)을 의미합니다.

| 직업/단계 | 상세 시나리오 |
| --- | --- |
| **마피아 – 밤** | (1) 플레이어가 `/select`로 타깃 지정 → `select.ms = target`, `select.lastMs = 1`. (2) 다른 마피아/접선 직업(`contact=true`)에게 "[ ▸ 선택 : target ]" DM. (3) 밤 채팅은 `contact=true`인 플레이어와 사망자에게만 `[마피아 팀]` 메시지 전송. |
| **마피아 – 낮** | 공개 투표/토론은 일반 시민과 동일. 사망 시 `deathList`에 추가하고 `say(room,"살해")`. |
| **경찰 – 밤** | `/select idx` 입력 → `select.경찰[name] = target`. 즉시 DM으로 "마피아/아님" 결과 회신. |
| **의사 – 밤** | `/select idx` → `select.의사[name] = target`. `onDay`에서 `target == mafiaTarget`인지 검사하여 살해 무효화. |
| **군인 – 마피아 공격 시** | `onDeath`: 첫 공격이면 `respawn=1`, 직업 공개 메시지, 사망 취소. 두 번째 공격부터 일반 사망. |
| **정치인 – 투표** | `voteCount=2`로 투표 시 두 표 반영. 사형 단계에서 `onVoteDeath`: "정치인은 투표로 죽지 않습니다" 메시지, `deathList` 미추가. |
| **영매 – 밤** | 모든 사망자에게 DM으로 영매 채팅 전달. `/select`로 사망자를 고르면 "그 사람 직업은 ..." DM, 대상에게 "성불" DM 후 `cantChat[target]=1`. |
| **연인** | 밤 채팅이 연인끼리만 공유. 마피아 공격으로 연인 A가 지목되면 `onDeath`에서 생존 중인 연인 B가 대신 사망(`deathList.push(B)`), 양쪽 prefix를 "연인"으로 강제. |
| **기자** | `/select` 시 `chains <= 1`이면 거부. 성공 시 다음 날 `mafiaFn.say`로 대상 직업 방송, 본인 `playerJob[name].canSelect=false`. |
| **건달** | `/select`로 협박 대상 지정 → 대상에게 DM, `cantVote`에 넣음. 다음 투표 한 번만 적용. |
| **도굴꾼** | `onDay`: 첫 밤(`chains==1`)에 마피아에게 죽은 사람 찾기 → 도굴꾼의 `playerJob`을 희생자의 직업으로 교체, 희생자는 시민/악인으로 강등. |
| **사립탐정** | `/select`로 대상 지정 → 대상이 능력을 사용했으면 `selectName[target]`을 조회해 "지목 : ○○" DM. |
| **테러리스트** | `/select`로 자폭 대상 저장. 마피아에게 죽으면 대상이 마피아이냐에 따라 동반 자폭 결정. 투표로 죽을 때 `onVoteDeath`: 반론 동안 대상 지정이 있으면 함께 제거. |
| **성직자** | `/select`로 사망자 선택 → 부활 성공 시 `deathList`에서 제거, 트릭/교주 상태도 정리. 실패 시 "부활 실패". 포교 면역: 교주가 시도하면 성직자에게 교주 정보 전달. |
| **마술사** | `/select`로 트릭 대상 저장(`lockSel`). 밤 종료 후 마술사가 죽으면 `trick`/`trickList` 업데이트, 대상 직업/팀을 마술사 팀으로 변경. |
| **스파이** | 마피아 조사 성공 시 `contact=true`, 마피아와 상호 알림, 마피아 채널 참여. 군인 조사 시 대상에게 조사 사실 DM. |
| **마담** | 낮 투표 대상에게 유혹 메시지. 대상이 마피아이면 접선(`contact=true`). 시민이면 `cantUse`에 넣어 능력 봉인. 마담 사망 시 `cantUse` 초기화. |
| **도둑** | 투표 대상의 직업을 일시적으로 복사(마피아면 접선). 특수 직업(군인/교주 등)은 실패 시 피해자에게 알림. 밤 종료 후 일정 조건으로 직업 반환. |
| **짐승인간** | `/select`로 마피아 추적 대상 지정(`select.bms`). 마피아가 같은 대상을 고르면 접선+즉사 메시지. 짐승인간이 이미 접선 상태면 마피아 타깃 처리 우선. |
| **교주** | 홀수 밤에만 `/select`. 포교 성공 시 대상 `playerJob[..].type = "sect"`, 피해자에게 "포교당했습니다" DM, 교주는 대상 직업 인지. 성직자/마피아는 포교 불가. |

## 16. Go 구현 설계 초안

이식 시 고려해야 할 구조적 설계 요소:

### 16.1 상태 구조체

```go
type Job interface {
    Name() string
    Team() Team
    OnChat(ctx *Context, msg string)
    OnSelect(ctx *Context, target string) error
    OnNightEnd(ctx *Context)
    OnDeath(ctx *Context, cause Cause)
    OnVoteDeath(ctx *Context) bool
    CanSelectDead() bool
    MaxSelect() int
}

type RoomState struct {
    Players map[string]*Client
    Alive   map[string]bool
    Jobs    map[string]JobInstance
    Prefix  map[string]map[string]string
    Selects map[SelectKey]string
    Trick   map[string]string
    TrickList map[string][]string
    CantUse map[string]bool
    CantVote map[string]bool
    CantChat map[string]bool
    Vote    map[string]int
    DeathTarget *DeathTarget
    Timers struct {
        Phase *time.Timer
        Interval *time.Ticker
    }
}
```

### 16.2 이벤트 파이프라인

1. **Ingress**: WebSocket 메시지 → `ClientMessage` → `Room.handleMessage` → switch-case로 룸 이벤트 큐에 함수 enqueue.
2. **State Machine**: 룸 goroutine이 큐에서 함수 실행. 이 함수가 Job 훅 호출, 상태 갱신, 브로드캐스트 수행.
3. **Timers**: `Room.setPhaseTimer`와 `Room.startTicker`가 `time.AfterFunc`/`time.Ticker`로 이벤트 enqueue.
4. **Jobs**: 각 직업은 `Job` 인터페이스 구현체로, 공통 컨텍스트(`Room`, `player`, `target`)를 받아 상태맵을 조작.
5. **Broadcast**: `Room.broadcast`/`Room.broadcastTeam`/`Client.push` 조합으로 `ServerEvent` 송신.

### 16.3 직업 레지스트리

```go
var jobRegistry = map[string]func(room *Room, player string) Job{
    "마피아": NewMafia,
    "경찰": NewDetective,
    ...
}
```

배정 시 `jobRegistry[name]`로 Job 인스턴스를 생성해 `RoomState.Jobs[player]`에 저장. 각 Job은 내부적으로 필요한 상태(예: 마술사의 트릭 대상)를 구조체 필드로 유지.

### 16.4 타이머/찬반 설계

| 단계 | Go 구현 | 상태 영향 |
| --- | --- | --- |
| 밤 타이머 | `time.AfterFunc` → `Room.resolveNight()` enqueue | `select` 해석, 사망 처리, 도굴/포교 등 |
| 낮 토론 | `time.NewTicker(1s)` → `eventTimer--`, 30/10초 이벤트 시 `broadcast`. 종료 시 ticker stop. | `eventTimer`, `editTime` 관리 |
| 투표/변론/찬반 | 각 단계별 `time.AfterFunc`. 변론/찬반은 `Room.beginDefense` → `Room.beginExecutionVote` → `Room.resolveDefense`. | `DeathTarget`, `ExecutionState` |

### 16.5 인증/닉네임

- 서버 플래그 `--ws-auth-key`로 헤더 검증.
- 닉네임 충돌 방지를 위해 `RoomManager.Attach`에서 중복 체크 및 에러 반환.

---

이 설계 초안을 기반으로, 직업별 `Job` 구현과 상태 머신을 차례로 이식할 수 있습니다. 다음 단계로는 직업별 Go 인터페이스 설계 및 테스트 전략을 세분화할 예정입니다.
