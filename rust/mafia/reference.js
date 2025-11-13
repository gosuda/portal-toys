


var mafiaFn = {
	say: function(room, msg) {
		if (mafiaList.indexOf(room) == -1) return;
		for (var i in mafia[room].list) {
			reply(mafia[room].list[i], msg);
		}
	},
	chat: function(sender, msg) {
		let room = mafiaJoin[sender];
		let ogname = sender;


		if (mafiaList.indexOf(room) == -1) return;


		if (mafia[room].game == true) {
			if (mafia[room].cantChat[ogname] != null) return;
			if (mafia[room].trick[sender] != null) sender = mafia[room].trick[sender];


			if (mafia[room].deathList.indexOf(sender) != -1 || mafia[room].trickList[ogname] != null) {
				for (let player of mafia[room].deathList) {
					if (mafia[room].trick[player] != null) continue;
					if (sender != player) reply(player, "▸" + (mafia[room].list.indexOf(ogname) + 1) + " | 【" + mafiaFn.getPrefix(player, ogname, room) + "】\n▸ " + ogname + " ☠\n▸ " + msg);
				}
				for (let player of mafia[room].list) {
					if (mafia[room].playerJob[player][1] == "영매" && mafia[room].deathList.indexOf(player) == -1 && mafia[room].trickList[player] == null) reply(player, "▸" + (mafia[room].list.indexOf(ogname) + 1) + " | 【" + mafiaFn.getPrefix(player, ogname, room) + "】\n▸ " + ogname + " ☠\n▸ " + msg);
				}
				return;
			}
			if (mafia[room].isDay == true) {
				// Day Chat


				if (mafia[room].deathTarget.name != null && mafia[room].deathTarget.name != sender && mafia[room].deathTarget.say == true) return;


				for (let player of mafia[room].list) {
					if (mafia[room].trick[player] != null && ogname == player) reply(player, "▸" + (mafia[room].list.indexOf(sender) + 1) + " | 【" + mafiaFn.getPrefix(player, sender, room) + "】\n▸ " + sender + "\n▸ " + msg);
					if (ogname != player) reply(player, "▸" + (mafia[room].list.indexOf(sender) + 1) + " | 【" + mafiaFn.getPrefix(player, sender, room) + "】\n▸ " + sender + "\n▸ " + msg);
				}
			} else {
				// Night Chat
				if (mafia[room].playerJob[sender][0].onChat != null) eval(mafia[room].playerJob[sender][0].onChat)(sender, msg);
			}
		} else {
			for (let player of mafia[room].list)
				if (sender != player) reply(player, "▸ 【" + (mafia[room].list.indexOf(ogname) + 1) + "】\n▸ " + ogname + "\n▸ " + msg);
		}
	},
	getPrefix: function(sender, str, room) {
		if (mafia[room].game == true) {
			if (mafia[room].playerJob[sender][0].name == mafia[room].playerJob[str][0].name) return mafia[room].playerJob[str][0].name;
			return (mafia[room].prefix[sender][str] == null ? "메모 없음" : mafia[room].prefix[sender][str]);
		} else return "메모 없음";
	},
	setPrefix: function(sender, num, str, room, subject) {
		if (subject == true) {
			for (var i in mafia[room].list) {
				mafia[room].prefix[mafia[room].list[i]][mafia[room].list[num]] = str;
			}
		} else {
			mafia[room].prefix[sender][mafia[room].list[num]] = str;
		}
	},
	getSelect: function(job) {
		if (job == null) return [];
		return Object.keys(job).map((l) => job[l]);
	},
	makeRoom: function(room) {
		if (mafiaList.indexOf(room) != -1) {
			reply(room, "[ 이미 " + room + " 이름으로 생성된 방이 존재합니다. ]");
			return;
		}
		mafiaList.push(room);
		mafia[room] = {
			game: false,
			isDay: true,
			isVote: false,


			specialJob: {
				시민: {
					type: "citizen",
					desc: "아무 능력이 없습니다.",
					canSelect: false
				},
				악인: {
					type: "mafia",
					desc: "아무 능력이 없습니다.",
					canSelect: false
				}
			},
			// Will add : onDeath job , onVote
			job: {
				마피아: {
					type: "mafia",
					desc: "밤마다 플레이어 한 명을 죽일 수 있다.",
					impor: true,
					onChat: function(name, msg) {
						let room = mafiaJoin[name];
						for (let player of mafia[room].list) {
							if (mafia[room].playerJob[player][0].contact == true && name != player) reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + "\n▸ " + msg);
						}
						for (let player of mafia[room].deathList) {
							reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [마피아 팀]\n▸ " + msg);
						}
					},
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						mafia[room].select.ms = mafia[room].list[index];
						mafia[room].select.lastMs = 1;
						for (let player of mafia[room].list) {
							if (mafia[room].playerJob[player][0].contact == true && mafia[room].playerJob[name][1] == "마피아") reply(player, "[ ▸ 선택 : " + mafia[room].list[index] + " ]");
							if (mafia[room].playerJob[player][0].name == "짐승인간" && mafia[room].playerJob[name][0].contact == true) reply(player, "[ ▸ 선택 : " + mafia[room].list[index] + " ]");
						}
					}
				},
				경찰: {
					type: "citizen",
					desc: "밤마다 한 사람을 조사하여 그 사람의 직업이 마피아인지 여부를 알 수 있다.[",
					impor: true,
					canSelect: 2,
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						if (mafia[room].playerJob[mafia[room].list[index]][0].name == "마피아") reply(name, "[ " + mafia[room].list[index] + " 님은 마피아 입니다. ]");
						else reply(name, "[ " + mafia[room].list[index] + " 님은 마피아가 아닙니다. ]");
					}
				},
				의사: {
					type: "citizen",
					desc: "밤마다 한 사람을 지목하여 대상이 마피아에게 공격받을 경우, 대상을 치료한다.",
					impor: true,
					canSelect: true
				},


				// Special Job
				군인: {
					type: "citizen",
					desc: "마피아의 공격을 한번 버텨낼 수 있다.",
					canSelect: false,
					onDeath: function(room, name) {
						if (mafia[room].playerJob[name][0].respawn == null) {
							mafia[room].playerJob[name][0].respawn = 1;
							mafiaFn.setPrefix(null, mafia[room].list.indexOf(name), mafia[room].playerJob[name][1], room, true);
							mafiaFn.say(room, "[ " + name + " 님이 마피아의 공격을 버텨 냈습니다. ]");
						} else {
							mafiaFn.say(room, "[ " + name + " 님이 살해당했습니다. ]");
							mafia[room].deathList.push(name);
						}
					}
				},
				정치인: {
					type: "citizen",
					desc: "플레이어간의 투표로 처형당하지 않는다.\n정치인의 투표권은 두 표로 인정된다.",
					canSelect: false,
					voteCount: 2,
					onVote: function(name, index) {
						let room = mafiaJoin[name];
						mafia[room].vote[mafia[room].list[index]]++;
					},
					onVoteDeath: function(name) {
						let room = mafiaJoin[name];
						mafiaFn.setPrefix(null, mafia[room].list.indexOf(name), mafia[room].playerJob[name][1], room, true);
						mafiaFn.say(room, "[ 정치인은 투표로 죽지 않습니다. ]");
					}
				},
				영매: {
					type: "citizen",
					desc: "죽은 사람이 하는 채팅을 들을 수 있으며, 밤에 죽은사람과 대화를 할 수 있다.\n밤마다 죽은 사람 한명을 선택하여 그 사람의 직업을 알아내고 성불 상태로 만든다. (되살리기 불가능)",
					canSelect: 2,
					canDeadSelect: true,
					onChat: function(name, msg) {
						for (let player of mafia[room].deathList) {
							reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [영매]\n▸ " + msg);
						}
					},
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						let target = mafia[room].list[index];
						reply(name, "[ " + target + " 플레이어를 성불했습니다.\n그 사람의 직업은 " + mafia[room].playerJob[target][1] + " ]");
						reply(target, "[ 성불 당하였습니다.\n더 이상 채팅을 칠 수 없습니다. ]");
						mafia[room].cantChat[target] = 1;
					}
				},
				연인: {
					type: "citizen",
					desc: "밤에 다른 연인과 서로 대화가 가능하다.\n연인 두 명이 모두 생존하고 있을 때,연인 한명이 마피아에게 지목당할 경우 다른 연인이 대신 죽게 된다.",
					canSelect: 2,
					count: 2,
					onChat: function(name, msg) {
						let room = mafiaJoin[name];
						for (let player of mafia[room].list) {
							if (mafia[room].playerJob[player][1] == mafia[room].playerJob[name][1] && name != player && mafia[room].deathList.indexOf(player) == -1) reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + "\n▸ " + msg);
						}
						for (let player of mafia[room].deathList) {
							reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [연인]\n▸ " + msg);
						}
					},
					onDeath: function(room, name) {
						let target;
						for (let player of mafia[room].list) {
							if (mafia[room].playerJob[player][1] == mafia[room].playerJob[name][1] && name != player && mafia[room].deathList.indexOf(player) == -1) target = player;
						}
						if (target == null) {
							mafiaFn.say(room, "[ " + name + " 님이 살해당했습니다. ]");
							mafia[room].deathList.push(name);
						} else {
							mafiaFn.say(room, "[ " + target + " 님이 연인인 " + name + " 님을 감싸고 살해당했습니다. ]");
							mafia[room].deathList.push(target);
							mafiaFn.setPrefix(null, mafia[room].list.indexOf(name), mafia[room].playerJob[name][1], room, true);
							mafiaFn.setPrefix(null, mafia[room].list.indexOf(target), mafia[room].playerJob[target][1], room, true);
						}


					}
				},
				기자: {
					type: "citizen",
					desc: "첫날 밤이 아닌 밤에 한 명을 선택하여 취재해 다음 날 그 사람의 직업을 모두에게 공개한다. (1회용)",
					canSelect: true,
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						if (mafia[room].chains <= 1) {
							reply(name, "[ 첫날밤은 취재할 수 없습니다. ]");
							delete mafia[room].select[mafia[room].playerJob[name][1]][name];
						}
					},
					onDay: function(room, name, sel, job) {
						if (mafia[room].deathList.indexOf(sel) != -1) return;
						mafiaFn.say(room, "[ 특종입니다! " + mafia[room].select[job][name] + " 님의 직업이 " + mafia[room].playerJob[mafia[room].select[job][name]][0].name + " 이라는 소식입니다! ]");
						mafia[room].playerJob[name][0].canSelect = false;
						mafiaFn.setPrefix(null, mafia[room].list.indexOf(sel), mafia[room].playerJob[sel][0].name, room, true);
					}
				},
				건달: {
					type: "citizen",
					desc: "밤에 지목된 플레이어는 다음 날 찬반 투표를 포함한 모든 투표를 할 수 없다.",
					canSelect: 2,
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						reply(name, "[ " + mafia[room].list[index] + " 님을 협박했습니다. ]");
						reply(mafia[room].list[index], "[ 건달에게 협박 당했습니다.\n다음 투표에는 참가하지 못합니다. ]");
						mafia[room].cantVote.push(mafia[room].list[index]);
					}
				},
				도굴꾼: {
					type: "citizen",
					desc: "첫날 마피아에게 살해당한 사람의 직업을 얻는다.",
					canSelect: false
				},
				사립탐정: {
					type: "citizen",
					desc: "밤에 한 사람을 조사하여 그 사람이 능력 사용 대상으로 누굴 선택하는지 볼 수 있다.",
					canSelect: 2,
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						let target = mafia[room].list[index];
						if (mafia[room].selectName[target] != null)
							reply(name, "[ ▸ 지목 : " + mafia[room].selectName[target] + " ]");
					}
				},
				테러리스트: {
					type: "citizen",
					desc: "밤마다 플레이어 한 명을 지목하여 해당 플레이어가 마피아일 때, 자신이 마피아의 공격을 받을 경우 지목한 마피아와 함께 사망한다.\n투표로 죽을 때, 최후에 반론 시간 때 플레이어 한 명을 골라 같이 처형될 수 있다.",
					canSelect: true,
					onDeath: function(room, name) {
						let select = mafia[room].select[mafia[room].playerJob[name][1]];
						if (select == null) {
							mafiaFn.say(room, "[ " + name + " 님이 살해당했습니다. ]");
							mafia[room].deathList.push(name);
						} else {
							if (select[name] == null) {
								mafiaFn.say(room, "[ " + name + " 님이 살해당했습니다. ]");
								mafia[room].deathList.push(name);


							} else if (mafia[room].playerJob[select[name]][0].name == "마피아") {
								mafiaFn.say(room, "[ 테러리스트 " + name + " 님이 마피아 " + mafia[room].select[mafia[room].playerJob[name][1]][name] + " 님과 함께 자폭하였습니다. ]");
								mafia[room].deathList.push(name);
								mafia[room].deathList.push(select[name]);
								mafiaFn.setPrefix(null, mafia[room].list.indexOf(name), mafia[room].playerJob[name][1], room, true);
								mafiaFn.setPrefix(null, mafia[room].list.indexOf(select[name]), mafia[room].playerJob[select[name]][1], room, true);


							} else {
								mafiaFn.say(room, "[ " + name + " 님이 살해당했습니다. ]");
								mafia[room].deathList.push(name);
							}
						}
					},
					onDeathTarget: function(name, index) {
						let room = mafiaJoin[name];
						if (mafia[room].list[index] == name) return;


						if (mafia[room].select[mafia[room].playerJob[name][1]] == null) mafia[room].select[mafia[room].playerJob[name][1]] = {};
						mafia[room].select[mafia[room].playerJob[name][1]][name] = mafia[room].list[index];


						reply(name, "[ ▸ 선택 : " + mafia[room].list[index] + " ]");
					},
					onVoteDeath: function(name) {
						let room = mafiaJoin[name];
						if (mafia[room].select[mafia[room].playerJob[name][1]] != null) {
							if (mafia[room].select[mafia[room].playerJob[name][1]][name] != null) {
								mafiaFn.say(room, "[ " + name + " 님이 " + mafia[room].select[mafia[room].playerJob[name][1]][name] + " 님과 함께 자폭하였습니다. ]");
								mafia[room].deathList.push(name);
								mafia[room].deathList.push(mafia[room].select[mafia[room].playerJob[name][1]][name]);
								return;
							}
						}


						mafiaFn.say(room, "[ " + mafia[room].deathTarget.name + "님이 투표로 인해 처형 당하셨습니다. ]");
						mafia[room].deathList.push(mafia[room].deathTarget.name);
					}
				},
				성직자: {
					type: "citizen",
					desc: "죽은 플레이어 한명을 밤에 선택하여 부활시킨다. (1회용)\n교주에게 포교당하지 않는다. (포교 시도당할 경우 교주가 누군지 알 수 있다.)",
					canSelect: true,
					canDeadSelect: true,
					onDay: function(room, name, sel, job) {


						if (mafia[room].cantChat[sel] == null) {
							mafiaFn.say(room, "[ " + sel + " 님이 부활하셨습니다. ]");
							mafia[room].playerJob[name][0].canSelect = false;
							module.rA(mafia[room].deathList, sel);


							if (mafia[room].trick[sel] != null) {
								module.rA(mafia[room].trickList[mafia[room].trick[sel]], sel);
								if (mafia[room].trickList[mafia[room].trick[sel]].length == 0) {
									delete mafia[room].playerJob[mafia[room].trick[sel]][0].trickTeam;
									delete mafia[room].trickList[mafia[room].trick[sel]];
								}
								delete mafia[room].trick[sel];
							}
						} else {
							mafiaFn.say(room, "[ 부활에 실패했습니다. ]");
							mafia[room].playerJob[name][0].canSelect = false;
						}


					}
				},
				마술사: {
					type: "citizen",
					desc: "밤에 플레이어 한 명에게 트릭을 걸어 자신이 사망할 때 해당 플레이어와 자신을 바꿔치기한다. (1회용)",
					canSelect: 2,
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						mafia[room].playerJob[name].canSelect = false;
						mafia[room].lockSel[name] = mafia[room].list[index];
						reply(name, "[ " + mafia[room].list[index] + " 님에게 트릭을 걸었습니다. ]");
					},
					onAnyDeath: function(room, name) {
						if (mafia[room].lockSel[name] != null && mafia[room].deathList.indexOf(mafia[room].lockSel[name]) == -1) {
							mafia[room].trick[name] = mafia[room].lockSel[name];
							if (mafia[room].trickList[mafia[room].lockSel[name]] == null) mafia[room].trickList[mafia[room].lockSel[name]] = [];


							mafia[room].playerJob[mafia[room].lockSel[name]][0].trickTeam = mafia[room].playerJob[name][0].type;
							mafia[room].trickList[mafia[room].lockSel[name]].push(name);


							reply(name, "[ 마술사 " + name + " 님의 트릭에 의해 " + mafia[room].lockSel[name] + " 님이 대신 사망하였습니다. ]");
							reply(mafia[room].lockSel[name], "[ 마술사 " + name + " 님의 트릭에 의해 " + mafia[room].lockSel[name] + " 님이 대신 사망하였습니다. ]");
						}
					}
				},


				// Sub Mafia
				스파이: {
					type: "citizen",
					desc: "밤마다 플레이어 한 명을 골라 그 사람의 직업을 알아낼 수 있다.\n능력을 사용한 대상이 마피아일 경우 접선한다.",
					canSelect: 2,
					subMafia: true,
					contact: false,
					condi: 6,
					impor: true,
					onChat: function(name, msg) {
						let room = mafiaJoin[name];


						if (mafia[room].playerJob[name][0].contact == true) {
							let room = mafiaJoin[name];
							for (let player of mafia[room].list) {
								if (mafia[room].playerJob[player][0].type == "mafia" && name != player) reply(player, "▸ " + mafiaFn.getPrefix(player, name, room) + "\n▸ [" + (mafia[room].list.indexOf(name) + 1) + "] " + name + "\n▸ " + msg);
							}
							for (let player of mafia[room].deathList) {
								reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [마피아 팀]\n▸ " + msg);
							}
						}
					},
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						let target = mafia[room].list[index];
						if (mafia[room].playerJob[target][0].name == "마피아" && mafia[room].playerJob[name][0].contact == false) {
							reply(name, "[ 그 사람의 직업은 마피아 ]");
							reply(name, "[ 접선했습니다. ]");
							mafia[room].playerJob[name][0].contact = true;


							mafia[room].list.forEach(function(l) {
								if (mafia[room].playerJob[l][0].name == "마피아") {
									reply(l, "[ " + name + " 스파이와 접선했습니다. ]");
									mafiaFn.setPrefix(l, mafia[room].list.indexOf(name), mafia[room].playerJob[name][1], room);
									mafiaFn.setPrefix(name, mafia[room].list.indexOf(l), mafia[room].playerJob[l][0].name, room);
								}
							});


						} else if (mafia[room].playerJob[target][0].name == "군인") {
							reply(name, "[ 그 사람의 직업은 군인 ]");
							reply(target, "[ 스파이 " + name + " 님이 당신을 조사했습니다. ]");
							mafiaFn.setPrefix(target, mafia[room].list.indexOf(name), mafia[room].playerJob[name][1], room);
						} else {
							reply(name, "[ 그 사람의 직업은 " + mafia[room].playerJob[target][0].name + " ]");
							mafiaFn.setPrefix(name, mafia[room].list.indexOf(target), mafia[room].playerJob[target][0].name, room);
						}
					}
				},
				마담: {
					type: "citizen",
					desc: "낮에 투표한 사람을 마담이 죽기전까지 능력을 사용하지 못하도록 한다.\n마피아를 유혹할 경우 접선한다.",
					canSelect: 3,
					subMafia: true,
					contact: false,
					condi: 6,
					impor: true,
					onAnyDeath: function(room, name) {
						mafia[room].cantUse = [];
					},
					onVote: function(name, index) {
						let room = mafiaJoin[name];
						let target = mafia[room].list[index];
						if (name == target) return;


						reply(name, "[ " + target + " 님을 유혹했습니다. ]");
						if (mafia[room].playerJob[target][0].name == "마피아") {
							// if Mafia
							if (mafia[room].playerJob[name][0].contact == false) {
								mafia[room].playerJob[name][0].contact = true;
								reply(name, "[ 접선했습니다. ]");


								mafia[room].list.forEach(function(l) {
									if (mafia[room].playerJob[l][0].name == "마피아") {
										reply(l, "[ " + name + " 스파이와 접선했습니다. ]");
										mafiaFn.setPrefix(l, mafia[room].list.indexOf(name), mafia[room].playerJob[name][1], room);
										mafiaFn.setPrefix(name, mafia[room].list.indexOf(l), mafia[room].playerJob[l][0].name, room);
									}
								});
							}


						} else {
							reply(target, "[ 마담에게 유혹 당했습니다.\n마담이 죽을 때 까지 능력을 사용하지 못합니다. ]");
							mafia[room].cantUse.push(target);
						}


					}
				},
				도둑: {
					type: "citizen",
					desc: "투표한 플레이어의 능력을 훔쳐 다음날 낮까지 사용할 수 있다.\n마피아 직업을 훔칠 경우, 마피아와 접선한다.",
					canSelect: 3,
					subMafia: true,
					contact: false,
					condi: 6,
					impor: true,
					onChat: function(name, msg) {
						let room = mafiaJoin[name];


						if (mafia[room].playerJob[name][0].contact == true) {
							let room = mafiaJoin[name];
							for (let player of mafia[room].list) {
								if (mafia[room].playerJob[player][0].type == "mafia" && name != player) reply(player, "▸ " + mafiaFn.getPrefix(player, name, room) + "\n▸ [" + (mafia[room].list.indexOf(name) + 1) + "] " + name + "\n▸ " + msg);
							}
							for (let player of mafia[room].deathList) {
								reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [마피아 팀]\n▸ " + msg);
							}
						}
					},
					onVote: function(name, index) {
						let room = mafiaJoin[name];
						let target = mafia[room].list[index];
						if (name == target) return;


						mafiaFn.setPrefix(name, mafia[room].list.indexOf(target), mafia[room].playerJob[target][0].name, room);


						if (mafia[room].playerJob[target][0].name == "교주") {
							mafiaFn.say(room, "[ 교주의 종소리가 울렸습니다. ]");
							reply(target, "[ " + mafia[room].playerJob[name][0].name + " " + name + " 님을 포교했습니다. ]");
							reply(name, "[ 교주 " + target + " 님에게 포교당했습니다. ]");
							mafia[room].playerJob[name][0].type = "sect";
							mafiaFn.setPrefix(target, mafia[room].list.indexOf(name), mafia[room].playerJob[name][0].name, room);
							return;
						} else if (mafia[room].playerJob[target][0].name == "군인") {
							reply(name, "[ 훔치는 데에 실패하였습니다. ]");
							reply(target, "[ 도둑 " + name + " 님이 직업을 훔치려고 시도했습니다. ]");
							mafiaFn.setPrefix(name, mafia[room].list.indexOf(target), mafia[room].playerJob[target][1], room, false);
						} else {


							reply(name, "[ " + target + " 님의 " + mafia[room].playerJob[target][0].name + " 직업을 훔쳤습니다. ]");


							if (mafia[room].playerJob[target][0].name == "마피아") {
								// if Mafia
								if (mafia[room].playerJob[name][0].contact == false) {
									mafia[room].playerJob[name][0].contact = true;
									reply(name, "[ 접선했습니다. ]");


									mafia[room].list.forEach(function(l) {
										if (mafia[room].playerJob[l][0].name == "마피아") {
											mafiaFn.setPrefix(name, mafia[room].list.indexOf(l), mafia[room].playerJob[l][0].name, room);
											mafiaFn.setPrefix(l, mafia[room].list.indexOf(name), mafia[room].playerJob[name][0].name, room);
											reply(l, "[ " + name + " 도둑과 접선했습니다. ]");
										}
									});
								}
							}


							let json = newObject(mafia[room].playerJob[target][0]);
							json.name = mafia[room].playerJob[name][1];
							json.type = mafia[room].playerJob[name][0].type;
							json.contact = mafia[room].playerJob[name][0].contact;
							if (json.onChat != null) json.onChat = mafia[room].playerJob[name][0].onChat;


							mafia[room].playerJob[name][1] = mafia[room].playerJob[target][0].name;
							mafia[room].playerJob[name][0] = json;
						}


					},
					onAnyDeath: function(room, name) {
						let json = newObject(mafia[room].job[mafia[room].playerJob[name][0].name]);
						json.type = mafia[room].playerJob[name][0].type;
						json.name = mafia[room].playerJob[name][0].name;
						json.contact = mafia[room].playerJob[name][0].contact;


						mafia[room].playerJob[name][1] = mafia[room].playerJob[name][0].name;
						mafia[room].playerJob[name][0] = json;
					},
					onDay: function(room, name, sel, job) {
						let json = newObject(mafia[room].job[mafia[room].playerJob[name][0].name]);
						json.type = mafia[room].playerJob[name][0].type;
						json.name = mafia[room].playerJob[name][0].name;
						json.contact = mafia[room].playerJob[name][0].contact;


						mafia[room].playerJob[name][1] = mafia[room].playerJob[name][0].name;
						mafia[room].playerJob[name][0] = json;
					},
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						if (mafia[room].playerJob[name][1] == "마피아") {
							for (let player of mafia[room].list) {
								if (mafia[room].playerJob[name][0].name == "마피아") reply(player, "[ ▸ 선택 : " + mafia[room].list[index] + " ]");
							}
						}
					}
				},
				짐승인간: {
					type: "citizen",
					desc: "자신이 밤에 선택한 사람을 마피아가 선택할 경우, 마피아와 접선한다.\n이 때, 대상에게 발동되는 처형 방해 효과를 무시한다.",
					canSelect: true,
					subMafia: true,
					contact: false,
					condi: 6,
					impor: true,
					onChat: function(name, msg) {
						let room = mafiaJoin[name];


						if (mafia[room].playerJob[name][0].contact == true) {
							let room = mafiaJoin[name];
							for (let player of mafia[room].list) {
								if (mafia[room].playerJob[player][0].type == "mafia" && name != player) reply(player, "▸ " + mafiaFn.getPrefix(player, name, room) + "\n▸ [" + (mafia[room].list.indexOf(name) + 1) + "] " + name + "\n▸ " + msg);
							}
							for (let player of mafia[room].deathList) {
								reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [마피아 팀]\n▸ " + msg);
							}
						}
					},
					onDeath: function(room, name) {
						mafiaFn.say(room, "[ 아무 일도 일어나지 않았습니다. ]");


						if (mafia[room].trickList[name] != null) return;


						if (mafia[room].playerJob[name][0].contact == false) {
							mafia[room].playerJob[name][0].contact = true;
							reply(name, "[ 길들여 졌습니다. ]");


							mafia[room].list.forEach(function(l) {
								if (mafia[room].playerJob[l][0].name == "마피아") {
									mafiaFn.setPrefix(name, mafia[room].list.indexOf(l), mafia[room].playerJob[l][0].name, room);
									mafiaFn.setPrefix(l, mafia[room].list.indexOf(name), mafia[room].playerJob[name][0].name, room);
									reply(l, "[ " + name + " 짐승인간과 접선했습니다. ]");
								}
							});
						}


					},
					onSelect: function(name, index) {
						let room = mafiaJoin[name];


						mafia[room].select.lastMs = 2;
						mafia[room].select.bms = name;
						if (mafia[room].playerJob[name][0].contact == true) mafia[room].select.ms = mafia[room].list[index];
						if (mafia[room].playerJob[name][0].type == "mafia") {
							for (let player of mafia[room].list)
								if (mafia[room].playerJob[name][0].name == "마피아") reply(player, "[ ▸ 선택 : " + mafia[room].list[index] + " ]");
						}
					}
				},


				// Sect Team
				교주: {
					type: "sect",
					desc: "홀수번째 밤마다 마피아, 성직자에 해당하지 않는 플레이어를 포교상태로 만들 수 있다.\n포교한 대상의 직업을 알 수 있으며, 밤마다 일방적으로 대화를 전달할 수 있다.",
					canSelect: 2,
					sect: true,
					condi: 9,
					impor: true,
					onChat: function(name, msg) {
						let room = mafiaJoin[name];
						for (let player of mafia[room].list) {
							if (mafia[room].playerJob[player][0].type == "sect") reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [교주]\n▸ " + msg);
						}
						for (let player of mafia[room].deathList) {
							reply(player, "▸" + (mafia[room].list.indexOf(name) + 1) + " | 【" + mafiaFn.getPrefix(player, name, room) + "】\n▸ " + name + " [교주 팀]\n▸ " + msg);
						}
					},
					onSelect: function(name, index) {
						let room = mafiaJoin[name];
						let target = mafia[room].list[index];
						if (mafia[room].chains % 2 != 1) {
							reply(name, "[ 홀수번째 밤이 아닙니다. ]");
							delete mafia[room].select[mafia[room].playerJob[name][1]][name];
							return;
						}


						mafiaFn.setPrefix(name, mafia[room].list.indexOf(target), mafia[room].playerJob[target][0].name, room);


						if (mafia[room].playerJob[target][0].name == "마피아") {
							reply(name, "[ 포교에 실패하였습니다. 해당 플레이어는 마피아입니다. ]");
							return;
						}
						if (mafia[room].playerJob[target][0].name == "성직자") {
							reply(name, "[ 포교에 실패하였습니다. 해당 플레이어는 성직자입니다.\n정체가 들켰습니다. ]");
							reply(target, "[ 교주 " + name + " 님에게 포교 시도 당했습니다. ]");
							mafiaFn.setPrefix(target, mafia[room].list.indexOf(name), mafia[room].playerJob[name][0].name, room);
							return;
						}


						reply(name, "[ " + mafia[room].playerJob[name][0].name + " " + name + " 님을 포교했습니다. ]");
						mafiaFn.say(room, "[ 교주의 종소리가 울렸습니다. ]");
						mafiaFn.setPrefix(target, mafia[room].list.indexOf(name), mafia[room].playerJob[name][0].name, room);
						reply(target, "[ 교주 " + name + " 님에게 포교당했습니다. ]");
						mafia[room].playerJob[target][0].type = "sect";
					}
				}
			},
			onDay: function(room) {


				let healVic = mafiaFn.getSelect(mafia[room].select.의사);
				let sel = mafia[room].select.ms;
				let lastSel = mafia[room].select.lastMs;
				let beastman = mafiaFn.getSelect(mafia[room].select.짐승인간);


				let check = false;


				if (sel != null) {
					if (beastman.indexOf(sel) != -1) {
						let bm = mafia[room].select.bms;
						if (mafia[room].playerJob[bm][0].contact == false) {
							mafia[room].playerJob[bm][0].contact = true;
							reply(bm, "[ 길들여 졌습니다. ]");


							mafia[room].list.forEach(function(l) {
								if (mafia[room].playerJob[l][0].name == "마피아") {
									mafiaFn.setPrefix(bm, mafia[room].list.indexOf(l), mafia[room].playerJob[l][0].name, room);
									mafiaFn.setPrefix(l, mafia[room].list.indexOf(bm), mafia[room].playerJob[bm][0].name, room);
									reply(l, "[ " + bm + " 짐승인간과 접선했습니다. ]");
								}
							});


							// Skip Another Event
							check = true;


							mafiaFn.say(room, "[ " + sel + " 님이 짐승인간에게 습격당했습니다. ]");
							mafia[room].deathList.push(sel);


							if (mafia[room].playerJob[sel][0].onAnyDeath != null) eval(mafia[room].playerJob[sel][0].onAnyDeath)(room, sel);


							// 도굴관련
							if (mafia[room].playerJob[sel][1] != "도굴꾼" && mafia[room].chains == 1) {
								let findArray = mafia[room].list.filter(p => mafia[room].playerJob[p][1] == "도굴꾼");
								if (findArray.length != 0) {
									mafia[room].playerJob[findArray[0]][0] = newObject(mafia[room].job[mafia[room].playerJob[sel][1]]);
									mafia[room].playerJob[findArray[0]][1] = mafia[room].playerJob[sel][1];


									reply(findArray[0], "[ " + mafia[room].playerJob[sel][1] + " 직업을 도굴했습니다. ]");
									let json = newObject(mafia[room].specialJob.악인);
									let text = "악인";
									if (mafia[room].playerJob[sel][0].type == "citizen")(json = newObject(mafia[room].specialJob.시민), text = "시민");
									if (mafia[room].playerJob[sel][0].type == "cult")(json.type = "cult", text = "포교된 악인");
									reply(sel, "[ 직업을 도굴당해서, " + text + "이 되었습니다. ]");
								}
							}


						} else if (lastSel == 2) {
							// Skip Another Event
							check = true;


							mafiaFn.say(room, "[ " + sel + " 님이 짐승인간에게 습격당했습니다. ]");
							mafia[room].deathList.push(sel);


							if (mafia[room].playerJob[sel][0].onAnyDeath != null) eval(mafia[room].playerJob[sel][0].onAnyDeath)(room, sel);
						}
					}


					if (check == false) {
						if (healVic.indexOf(sel) != -1) {
							mafiaFn.say(room, "[ " + sel + " 님이 의사의 도움으로 마피아의 공격을 버텨 냈습니다. ]");
						} else {
							if (mafia[room].playerJob[sel][0].onDeath != null)
								eval(mafia[room].playerJob[sel][0].onDeath)(room, sel);
							else {
								mafiaFn.say(room, "[ " + sel + " 님이 살해당했습니다. ]");
								mafia[room].deathList.push(sel);


								if (mafia[room].playerJob[sel][0].onAnyDeath != null) eval(mafia[room].playerJob[sel][0].onAnyDeath)(room, sel);


								// 도굴관련
								if (mafia[room].playerJob[sel][1] != "도굴꾼" && mafia[room].chains == 1) {
									let findArray = mafia[room].list.filter(p => mafia[room].playerJob[p][1] == "도굴꾼");
									if (findArray.length != 0) {
										mafia[room].playerJob[findArray[0]][0] = newObject(mafia[room].job[mafia[room].playerJob[sel][1]]);
										mafia[room].playerJob[findArray[0]][1] = mafia[room].playerJob[sel][1];


										reply(findArray[0], "[ " + mafia[room].playerJob[sel][1] + " 직업을 도굴했습니다. ]");
										let json = mafia[room].specialJob.악인;
										let text = "악인";
										if (mafia[room].playerJob[sel][0].type == "citizen")(json = mafia[room].specialJob.시민, text = "시민");
										if (mafia[room].playerJob[sel][0].type == "cult")(json.type = "cult", text = "포교된 악인");
										reply(sel, "[ 직업을 도굴당해서, " + text + " 이 되었습니다. ]");
									}
								}
							}
						}
					}
				} else {
					mafiaFn.say(room, "[ 아무 일도 일어나지 않았습니다. ]");
				}


				let selects = Object.keys(mafia[room].select);
				for (let s of selects) {
					if (typeof mafia[room].select[s] != "object") continue;
					for (let player of mafia[room].list) {
						if (mafia[room].select[s][player] != null && mafia[room].deathList.indexOf(player) != -1)
							delete mafia[room].select[s][player];
					}
				}


				let array = Object.keys(mafia[room].select);
				for (let obj of array) {
					if (typeof mafia[room].select[obj] != "object") continue;
					for (let obj1 of Object.keys(mafia[room].select[obj])) {
						if (mafia[room].job[obj].onDay != null)
							eval(mafia[room].job[obj].onDay)(room, obj1, mafia[room].select[obj][obj1], obj);
					}
				}


				mafia[room].select = {};
				mafia[room].selectName = {};
			},
			onSelect: function(name, index) {
				let room = mafiaJoin[name];
				if (mafia[room].select.사립탐정 != null) {
					let obj = jsonL(mafia[room].select.사립탐정);
					obj.forEach(function(l, i) {
						if (mafia[room].select.사립탐정[l] == name) reply(l, "[ ▸ 지목 : " + mafia[room].list[index] + " ]");
					});
				}
			},
			onVote: function(name, index) {


			},


			trick: {},
			trickList: {},
			prefix: {},
			playerJob: {},
			ogPlayerJob: {},
			cantUse: [],
			select: {},
			selectName: {},


			lockSel: {},


			vote: {},
			voteList: [],
			cantVote: [],
			isExecution: false,
			isExecuList: [],
			editTime: [],
			deathTarget: {},
			list: [room],
			deathList: [],
			cantChat: {},
			chains: 0,


			eventTimer: 0
		};
		mafiaJoin[room] = room;
		reply(room, "[ " + room + " 이름의 방이 생성 되었습니다. 방 번호는 [ " + (mafiaList.indexOf(room) + 1) + " ] 번 입니다. ]");
	},
	join: function(sender, num) {
		if (mafiaJoin[sender] != undefined) {
			reply(sender, "[ 이미 [ " + mafiaJoin[sender] + " ] 방에 참가 되어 있습니다. ]");
			return;
		}
		if (mafiaList[num] == undefined) {
			reply(sender, "[ 해당하는 방이 존재하지 않습니다. 방 목록을 확인 해주세요. ]");
			return;
		}
		if (mafia[mafiaList[num]].game == true) {
			reply(sender, "[ 이미 게임 중인 방 입니다. ]");
			return;
		}
		mafia[mafiaList[num]].list.push(sender);
		mafiaJoin[sender] = mafiaList[num];
		mafiaFn.say(mafiaList[num], "[ " + sender + " 님께서 [ " + mafiaList[num] + " ] 방에 참가 하셨습니다.\n▸ 인원은 " + mafia[mafiaList[num]].list.length + " 명 입니다. ]");
		mafiaFn.sayList(mafiaList[num]);
	},
	quit: function(sender) {
		if (mafiaJoin[sender] == undefined) {
			reply(sender, "[ 참가 중인 방이 없습니다. ]");
			return;
		}
		if (mafia[mafiaJoin[sender]].game == true) {
			reply(sender, "[ 게임 중에는 퇴장하실 수 없습니다. ]");
			return;
		}
		if (mafia[sender] != undefined) {
			mafiaFn.say(sender, "[ " + sender + " 님이 [ " + mafiaJoin[sender] + " ] 방에서 퇴장 하셨습니다.\n방장이 나가, 방이 삭제 되었습니다. ]");
			mafia[sender].list.forEach(function(l) {
				delete mafiaJoin[l];
			});
			module.rA(mafiaList, sender);
			delete mafia[sender];
		} else {
			mafiaFn.say(mafiaJoin[sender], "[ " + sender + " 님이 [ " + mafiaJoin[sender] + " ] 방에서 퇴장 하셨습니다. ]");
			module.rA(mafia[mafiaJoin[sender]].list, sender);
			mafiaFn.sayList(mafiaJoin[sender]);
			delete mafiaJoin[sender];
		}
	},
	kick: function(sender, num) {
		if (mafiaJoin[sender] != undefined) {
			if (mafia[sender] == undefined) {
				reply(sender, "[ 방장이 아닙니다. ]");
				return;
			}
			if (mafia[sender].game == true) return;
			if (mafia[sender].list[num] != sender) {
				if (mafia[sender].list[num] == undefined) {
					reply(sender, "[ 해당하는 번호의 플레이어가 존재하지 않습니다. ]");
					return;
				}
				mafiaFn.say(sender, "[ " + mafia[sender].list[num] + " 님이 [ " + sender + " ] 방에서 강제 퇴장 당하셨습니다. ]");
				module.rA(mafia[sender].list, mafia[sender].list[num]);
				mafiaFn.sayList(mafiaJoin[sender]);
				delete mafiaJoin[mafia[sender].list[num]];
			}
		}
	},
	start: function(sender) {
		if (mafiaJoin[sender] == null) {
			reply(sender, "[ 방이 존재하지 않습니다. ]");
			return;
		}
		if (mafia[sender] == null) {
			reply(sender, "[ 방장이 아닙니다. ]");
			return;
		}
		if (mafia[sender].list.length < 4) {
			reply(sender, "[ 최소 인원인 4명부터 시작이 가능합니다. ]");
			return;
		}
		if (mafia[sender].game == false) {
			mafia[sender].game = true;
			mafia[sender].job.마피아.count = 1;
			if (mafia[sender].list.length >= 8) mafia[sender].job.마피아.count = 2;
			if (mafia[sender].list.length >= 12) mafia[sender].job.마피아.count = 3;
			mafia[sender].list.forEach((l) => {
				mafia[sender].prefix[l] = {};
			});
			mafia[sender].cantChat = {};
			Object.keys(mafia[sender].job).forEach(function(l) {
				mafia[sender].job[l].name = l;
			});


			mafiaFn.say(sender, "[ " + sender + " 님께서 게임을 시작 하셨습니다.\n▸ 마피아 수 : ‹" + mafia[sender].job.마피아.count + "› ]");
			mafiaFn.drawJob(sender);
			mafiaFn.nextDay(sender);
		}
	},
	select: function(sender, index) {
		let room = mafiaJoin[sender];
		let json = mafia[room];
		let ogname = sender;


		if (json.game == false) return;


		// Magician Trick
		if (json.trickList[sender] != null) return;


		if (mafia[room].trick[sender] != null) sender = mafia[room].trick[sender];


		if (json.deathTarget.name != null && json.deathTarget.name == sender) {
			// Death Target Select
			if (mafia[room].playerJob[sender][0].onDeathTarget == null) return;


			if (json.playerJob[sender][0].canSelect == false || json.cantUse.indexOf(sender) != -1) return;


			if (mafia[room].playerJob[sender][0].onDeathTarget != null) {
				if (json.playerJob[sender][0].canSelect == 2 && json.select[json.playerJob[sender][1]] != null) return;
				if (json.playerJob[sender][0].canDeadSelect == null && json.deathList.indexOf(json.list[index]) != -1) return;
				if (json.playerJob[sender][0].canDeadSelect == true && json.deathList.indexOf(json.list[index]) == -1) return;


				if (json.list.indexOf(json.list[index]) == -1) {
					reply(sender, "[ " + (index + 1) + " 번 님은 존재하지 않습니다. ]");
					return;
				}


				eval(mafia[room].playerJob[sender][0].onDeathTarget)(sender, index);
			}
		} else if (json.isDay == true && json.isVote == true && json.voteList.indexOf(ogname) == -1 && json.cantVote.indexOf(sender) == -1) {


			if (json.deathList.indexOf(sender) != -1) return;
			if (json.deathList.indexOf(json.list[index]) != -1) return;
			if (json.list.indexOf(json.list[index]) == -1) return;


			// Vote Select
			mafiaFn.say(room, "[ " + mafia[room].list[index] + " 님 한 표! ]");
			mafia[room].voteList.push(ogname);
			if (json.vote[mafia[room].list[index]] == null) mafia[room].vote[mafia[room].list[index]] = 0;
			mafia[room].vote[mafia[room].list[index]]++;
			if (json.playerJob[sender][0].onVote != null && json.cantUse.indexOf(sender) == -1) {
				if (mafia[room].select[mafia[room].playerJob[sender][1]] == null) mafia[room].select[mafia[room].playerJob[sender][1]] = {};
				mafia[room].select[mafia[room].playerJob[sender][1]][sender] = mafia[room].list[index];
				eval(mafia[room].playerJob[sender][0].onVote)(sender, index);
				if (mafia[room].select[mafia[room].playerJob[sender][1]][sender] != null) eval(mafia[room].onVote)(sender, index);
			}


		} else if (json.isDay == false) {
			// Night Select


			// Magician Trick
			if (json.trick[ogname] != null) return;


			if (json.deathList.indexOf(sender) != -1 && json.playerJob[sender][0].deadSelect == null) return;


			if (json.playerJob[sender][0].canSelect == false || json.playerJob[sender][0].canSelect == 3 || json.cantUse.indexOf(sender) != -1) return;


			if (json.playerJob[sender][0].canSelect == 2 && json.select[json.playerJob[sender][1]] != null) return;
			if (json.playerJob[sender][0].canDeadSelect == null && json.deathList.indexOf(json.list[index]) != -1) return;
			if (json.playerJob[sender][0].canDeadSelect == true && json.deathList.indexOf(json.list[index]) == -1) return;


			if (json.list.indexOf(json.list[index]) == -1) {
				reply(sender, "[ " + (index + 1) + " 번 님은 존재하지 않습니다. ]");
				return;
			}


			if (mafia[room].select[mafia[room].playerJob[sender][1]] == null) mafia[room].select[mafia[room].playerJob[sender][1]] = {};
			mafia[room].select[mafia[room].playerJob[sender][1]][sender] = mafia[room].list[index];
			mafia[room].selectName[sender] = mafia[room].list[index];


			for (let player of json.list) {
				if (json.playerJob[sender][0].name == json.playerJob[player][0].name) reply(player, "[ ▸ 선택 : " + mafia[room].list[index] + " ]");
			}


			if (json.playerJob[sender][0].onSelect != null) eval(json.playerJob[sender][0].onSelect)(sender, index);
			if (mafia[room].select[mafia[room].playerJob[sender][1]][sender] != null) json.onSelect(sender, index);
		}
	},
	drawJob: function(room) {
		let jobs = [];
		let imporJobs = [];
		let subMafia = [];


		let array = module.getDifArr(mafia[room].list);
		for (let i = 0; i < jsonL(mafia[room].job).length; i++) {
			if (mafia[room].job[jsonL(mafia[room].job)[i]].condi > array.length) continue;


			if (mafia[room].job[jsonL(mafia[room].job)[i]].subMafia == true) {
				subMafia.push(jsonL(mafia[room].job)[i]);
				continue;
			}


			if (mafia[room].job[jsonL(mafia[room].job)[i]].impor) {
				if (mafia[room].job[jsonL(mafia[room].job)[i]].count != null) {
					for (let j = 0; j < mafia[room].job[jsonL(mafia[room].job)[i]].count; j++)
						imporJobs.push(jsonL(mafia[room].job)[i]);
				} else imporJobs.push(jsonL(mafia[room].job)[i]);
				continue;
			}
			if (mafia[room].job[jsonL(mafia[room].job)[i]].count != null) {
				for (let j = 0; j < mafia[room].job[jsonL(mafia[room].job)[i]].count; j++)
					jobs.push(jsonL(mafia[room].job)[i]);
			} else jobs.push(jsonL(mafia[room].job)[i]);
		}


		if (subMafia.length != 0) {
			let rand = module.randomArr(array);
			let randJob = module.randomArr(subMafia);
			mafia[room].playerJob[rand] = [newObject(mafia[room].job[randJob]), randJob];
			mafia[room].ogPlayerJob[rand] = randJob;
			module.rA(array, rand);
		}


		let imporCount = imporJobs.length;
		for (let i = 0; i < imporCount; i++) {
			let rand = module.randomArr(array);
			let randJob = module.randomArr(imporJobs);
			mafia[room].playerJob[rand] = [newObject(mafia[room].job[randJob]), randJob];
			mafia[room].ogPlayerJob[rand] = randJob;
			module.rA(imporJobs, randJob);
			module.rA(array, rand);
		}
		let count = array.length;
		for (let i = 0; i < count; i++) {
			let randJob = module.randomArr(jobs);
			if (randJob == null) continue;
			let count2 = (mafia[room].job[randJob].count == null ? 1 : mafia[room].job[randJob].count);
			if (array.length - count2 >= 0) {
				for (let j = 0; j < count2; j++) {
					let rand = module.randomArr(array);
					mafia[room].playerJob[rand] = [mafia[room].job[randJob], randJob];
					mafia[room].ogPlayerJob[rand] = randJob;
					module.rA(jobs, randJob);
					module.rA(array, rand);
				}
			} else {
				i--;
				for (let j = 0; j < count2; j++) {
					module.rA(jobs, randJob);
				}
			}
		}
	},
	sayList: function(room) {
		for (let i = 0; i < mafia[room].list.length; i++) {
			let arr = [];
			let player = mafia[room].list[i];
			let j = 0;
			for (let targetPlayer of mafia[room].list) {
				j++;
				arr.push("[ ▸" + (j) + " | " + targetPlayer + " (" + mafiaFn.getPrefix(player, targetPlayer, room) + ") " + (mafia[room].deathList.indexOf(targetPlayer) != -1 ? " 【죽음】" : "") + " ]");
			}
			reply(player, "───────────────\n" + arr.join("\n") + "\n───────────────");
		}
	},
	sayAbi: function(room) {
		for (let player of mafia[room].list) {
			if (mafia[room].deathList.indexOf(player) == -1 && mafia[room].trick[player] != -1) reply(player, "[ ⬩ 직업 : " + mafia[room].playerJob[player][1] + "\n ⬩ 능력 : " + mafia[room].playerJob[player][0].desc + " ]");
		}
	},
	sayJob: function(room) {
		let arr = [];
		for (let i = 0; i < mafia[room].list.length; i++) {
			let player = mafia[room].list[i];
			arr.push("[ ▸" + (i + 1) + " | " + player + " (" + mafia[room].ogPlayerJob[player] + ") ]");
		}
		mafiaFn.say(room, "───────────────\n" + arr.join("\n") + "\n───────────────");
	},
	nextDay: function(room) {
		mafia[room].isDay = !mafia[room].isDay;
		mafia[room].chains++;
		mafia[room].deathTarget = {};
		mafia[room].cantVote = [];
		mafia[room].editTime = [];
		mafia[room].vote = {};
		mafia[room].isExecution = false;


		mafiaFn.say(room, "[ " + mafia[room].chains + "번째 밤이 되었습니다. ]");
		mafiaFn.sayList(room);
		mafiaFn.sayAbi(room);


		// Timer
		mafiaFn.say(room, "[ 아침까지 25초 남았습니다. ]");
		setTimeout("mafia Night Timer" + room, function() {
			mafiaFn.say(room, "[ 아침까지 10초 남았습니다. ]");
			clearTimeout("mafia Night Timer" + room);
			setTimeout("mafia Night Timer" + room, function() {
				mafiaFn.proceed(room);
			}, 10000);
		}, 15000);
	},


	proceed: function(room) {
		mafia[room].isDay = !mafia[room].isDay;
		mafiaFn.say(room, "[ 낮이 되었습니다. ]");


		eval(mafia[room].onDay)(room);


		mafiaFn.checkGameOver(room);
		if (mafia[room].game == false) return;


		let count = mafia[room].list.filter(p => mafia[room].deathList.indexOf(p) == -1).length;


		// Timer
		mafia[room].eventTimer = count * 15;
		setInterval("mafia Vote Timer" + room, function() {
			mafia[room].eventTimer--;
			if (mafia[room].eventTimer == 30) mafiaFn.say(room, "[ 투표까지 30초 남았습니다. ]");
			if (mafia[room].eventTimer == 10) mafiaFn.say(room, "[ 투표까지 10초 남았습니다. ]");
			if (mafia[room].eventTimer <= 0) {
				clearInterval("mafia Vote Timer" + room);
				mafiaFn.vote(room);
			}
		}, 1000);
	},
	vote: function(room) {
		mafia[room].isVote = true;
		mafia[room].voteList = [];
		mafiaFn.sayList(room);
		mafiaFn.say(room, "[ 투표 시간이 되었습니다.\n채팅창에 투표할 플레이어 번호를 입력해 주세요. ]");


		// Timer
		setTimeout("mafia Day Timer" + room, function() {
			mafiaFn.say(room, "[ 최후의 반론까지 5초 남았습니다. ]");
			clearTimeout("mafia Day Timer" + room);
			setTimeout("mafia Day Timer" + room, function() {


				mafiaFn.checkGameOver(room);
				if (mafia[room].game == false) return;


				mafiaFn.totalVote(room);
			}, 5000);
		}, 10000);
	},
	totalVote: function(room) {
		mafia[room].isVote = false;
		let arr = Object.keys(mafia[room].vote).map((l) => mafia[room].vote[l]);
		let arr1 = Object.keys(mafia[room].vote).map((l) => mafia[room].vote[l]);


		if (arr.length == 0) {
			mafiaFn.nextDay(room);
			return;
		}


		arr.sort(function(a, b) {
			return b - a;
		});


		if (arr.length > 1)
			if (arr[0] == arr[1]) {
				mafiaFn.nextDay(room);
				return;
			}


		mafia[room].deathTarget = {
			name: Object.keys(mafia[room].vote)[arr1.indexOf(arr[0])],
			agree: 0,
			opposition: 0,
			say: true
		};
		mafiaFn.say(room, "[ " + mafia[room].deathTarget.name + " 님의 최후의 반론. ]");


		// Timer
		setTimeout("mafia Day Timer" + room, function() {
			mafiaFn.say(room, "[ 찬성 반대 투표까지 5초 남았습니다. ]");
			clearTimeout("mafia Day Timer" + room);
			setTimeout("mafia Day Timer" + room, function() {
				mafiaFn.agreeOpo(room);
			}, 5000);
		}, 10000);
	},
	agreeOpo: function(room) {
		mafia[room].isExecution = true;
		mafia[room].deathTarget.say = false;
		mafia[room].isExecuList = [];
		mafiaFn.say(room, "[ " + mafia[room].deathTarget.name + " 님의 사형을 \"찬성\" 혹은 \"반대\" 를 입력해 처형 여부를 결정해 주세요. ]");


		setTimeout("mafia Day Timer" + room, function() {
			mafiaFn.say(room, "[ 처형까지 5초 남았습니다. ]");
			clearTimeout("mafia Day Timer" + room);
			setTimeout("mafia Day Timer" + room, function() {
				mafia[room].deathTarget.opposition += mafia[room].list.filter(p => mafia[room].deathList.indexOf(p) == -1).length - (mafia[room].deathTarget.agree + mafia[room].deathTarget.opposition);
				if (mafia[room].deathTarget.opposition <= mafia[room].deathTarget.agree) {
					if (mafia[room].playerJob[mafia[room].deathTarget.name][0].onVoteDeath != null && mafia[room].cantUse.indexOf(mafia[room].deathTarget.name) == -1) eval(mafia[room].playerJob[mafia[room].deathTarget.name][0].onVoteDeath)(mafia[room].deathTarget.name);
					else {
						mafiaFn.say(room, "[ " + mafia[room].deathTarget.name + "님이 투표로 인해 처형 당하셨습니다. ]");
						mafia[room].deathList.push(mafia[room].deathTarget.name);
						if (mafia[room].playerJob[mafia[room].deathTarget.name][0].onAnyDeath != null) eval(mafia[room].playerJob[mafia[room].deathTarget.name][0].onAnyDeath)(room, mafia[room].deathTarget.name);
					}
				}


				mafiaFn.checkGameOver(room);
				if (mafia[room].game == false) return;


				mafiaFn.nextDay(room);
			}, 5000);
		}, 10000);
	},
	execAgree: function(sender) {
		let room = mafiaJoin[sender];
		let ogname = sender;
		// Magician Trick
		if (mafia[room].trickList[sender] != null) return;


		if (mafia[room].trick[sender] != null) sender = mafia[room].trick[sender];


		if (mafia[room].deathList.indexOf(sender) != -1 || mafia[room].isExecuList.indexOf(ogname) != -1 || mafia[room].isExecution == false || mafia[room].cantVote.indexOf(sender) != -1) return;
		mafia[room].deathTarget.agree++;
		reply(ogname, "[ " + mafia[room].deathTarget.name + " 님의 처형에 찬성 했습니다. ]");
		mafia[room].isExecuList.push(ogname);
	},
	execOppo: function(sender) {
		let room = mafiaJoin[sender];
		let ogname = sender;
		// Magician Trick
		if (mafia[room].trickList[sender] != null) return;
		if (mafia[room].trick[sender] != null) sender = mafia[room].trick[sender];


		if (mafia[room].deathList.indexOf(sender) != -1 || mafia[room].isExecuList.indexOf(ogname) != -1 || mafia[room].isExecution == false || mafia[room].cantVote.indexOf(sender) != -1) return;
		mafia[room].deathTarget.opposition++;
		reply(ogname, "[ " + mafia[room].deathTarget.name + " 님의 처형에 반대 했습니다. ]");
		mafia[room].isExecuList.push(ogname);
	},


	reductTime: function(sender) {
		let room = mafiaJoin[sender];


		// Magician Trick
		if (mafia[room].trickList[sender] != null) return;
		if (mafia[room].trick[sender] != null) sender = mafia[room].trick[sender];


		if (mafia[room].deathList.indexOf(sender) != -1 || mafia[room].editTime.indexOf(sender) != -1 || mafia[room].isDay == false || mafia[room].eventTimer <= 0) return;


		mafia[room].eventTimer -= 15;
		mafia[room].editTime.push(sender);
		mafiaFn.say(room, "[ " + sender + " 님이 시간을 단축 하셨습니다. ]");
	},
	addTime: function(sender) {
		let room = mafiaJoin[sender];


		// Magician Trick
		if (mafia[room].trickList[sender] != null) return;
		if (mafia[room].trick[sender] != null) sender = mafia[room].trick[sender];


		if (mafia[room].deathList.indexOf(sender) != -1 || mafia[room].editTime.indexOf(sender) != -1 || mafia[room].isDay == false || mafia[room].eventTimer <= 0) return;


		mafia[room].eventTimer += 15;
		mafia[room].editTime.push(sender);
		mafiaFn.say(room, "[ " + sender + " 님이 시간을 증가 하셨습니다. ]");
	},


	// Return Type: JSON
	getCanVote: function(room) {
		let json = {};
		for (let player of mafia[room].list) {
			if (mafia[room].deathList.indexOf(player) == -1) {


				if (mafia[room].playerJob[player][0].trickTeam != null) {
					if (json[mafia[room].playerJob[player][0].trickTeam] == null) json[mafia[room].playerJob[player][0].trickTeam] = 0;
					let count = (mafia[room].playerJob[player][0].voteCount == undefined ? 1 : mafia[room].playerJob[player][0].voteCount);
					json[mafia[room].playerJob[player][0].trickTeam] += count;
					continue;
				}


				if (mafia[room].playerJob[player][0].sect == true)
					json.aliveSect = true;




				if (mafia[room].playerJob[player][0].contact == true && mafia[room].playerJob[player][0].type == "citizen") {
					if (json.mafia == null) json.mafia = 0;
					let count = (mafia[room].playerJob[player][0].voteCount == undefined ? 1 : mafia[room].playerJob[player][0].voteCount);
					json.mafia += count;
					continue;
				}


				if (json[mafia[room].playerJob[player][0].type] == null) json[mafia[room].playerJob[player][0].type] = 0;
				let count = (mafia[room].playerJob[player][0].voteCount == undefined ? 1 : mafia[room].playerJob[player][0].voteCount);
				json[mafia[room].playerJob[player][0].type] += count;
			}
		}
		return json;
	},
	checkGameOver: function(room) {
		let count = mafiaFn.getCanVote(room);
		count.mafia == null ? count.mafia = 0 : null;
		count.sect == null ? count.sect = 0 : null;
		count.citizen == null ? count.citizen = 0 : null;


		if (count.citizen + count.sect <= count.mafia) mafiaFn.gameOver(1, room);
		else if (count.mafia == 0 && count.sect == 0) mafiaFn.gameOver(2, room);
		else if (count.mafia == 0) {
			if (count.aliveSect == true) {
				if (count.citizen <= count.sect) mafiaFn.gameOver(3, room);
			} else {
				if (count.citizen == 0) mafiaFn.gameOver(3, room);
			}
		}
	},
	gameOver: function(type, room) {
		if (type == 1) {
			// Mafia
			mafiaFn.say(room, "[ 마피아 팀이 게임에서 승리 하였습니다. ]");
			mafiaFn.sayJob(room);
		} else if (type == 2) {
			// Citizen
			mafiaFn.say(room, "[ 시민 팀이 게임에서 승리 하였습니다. ]");
			mafiaFn.sayJob(room);
		} else if (type == 3) {
			// Else (Sect)
			mafiaFn.say(room, "[ 교주 팀이 게임에서 승리 하였습니다. ]");
			mafiaFn.sayJob(room);
		}


		// Default Setting
		mafia[room].game = false;
		mafia[room].deathList = [];
		mafia[room].playerJob = {};
		mafia[room].chains = 0;
		mafia[room].isDay = true;
		mafia[room].isVote = false;
		mafia[room].vote = {};
		mafia[room].voteList = [];
		mafia[room].deathTarget = {};
		mafia[room].ogPlayerJob = {};
		mafia[room].editTime = [];
		mafia[room].lockSel = {};
		mafia[room].trick = {};
		mafia[room].trickList = [];
	}
};
