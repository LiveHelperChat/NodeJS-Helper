(function($){	
		var nodejshelper = {

				operatorForced : false,	
				isConnected : false,	
				socket : null, 
				
				intervalSyncTimeout : null,
				chatActivated : false,
				
				init : function(){
					this.socket = io.connect(nodejshelperHostConnect,{secure:nodejshelperConfig.secure});
					
					this.socket.on('connect', this.onConnected);
					this.socket.on('newmessage', this.onMessage);
					this.socket.on('syncforce', this.syncForce);
					this.socket.on('usertyping', this.usertyping);
					this.socket.on('operatortyping', this.operatortyping);
					this.socket.on('userleftchat', this.userleftchat);
					this.socket.on('userjoined', this.userjoined);
					this.socket.on('addfileupload', this.syncForce);
					this.socket.on('addfileuserupload', this.syncForce);
					this.socket.on('userpostedmessage', this.userpostedmessage);
					this.socket.on('userstartedpostmessage', this.userstartedpostmessage);
					this.socket.on('syncbackoffice', this.syncbackoffice);					
					this.socket.on('reconnect_error', this.onDisconect);				
					this.socket.on('connect_timeout', this.onDisconect);
				},
				
				setupForceTimeout : function() {	
					clearTimeout(this.intervalSyncTimeout);
					if (nodejshelperConfig.sync == true && this.chatActivated == false) {
						this.intervalSyncTimeout = setTimeout(function(){
							nodejshelper.operatorForced = true;
							lhinst.syncusercall();
						}, nodejshelperConfig.synctimeout*1000);
					}
				},
				
				syncForce : function(chat_id) {
					if (nodejshelper.isConnected == true) {
						if (lhinst.chat_id == chat_id) {
							nodejshelper.operatorForced = true;
							lhinst.syncusercall();
							nodejshelper.chatActivated = true;
						} else {
							lhinst.syncadmincall();
						}
					}
				},
				
				onMessage : function(messageData) {	
					if (nodejshelper.isConnected == true) {
						if (lhinst.chat_id) {						
							if (typeof messageData.data.data !== 'undefined') {	
								if ($('#messagesBlock').find('.usr-tit').size() > 0 && $('#messagesBlock').find('.usr-tit').last().attr('data-sender') == messageData.data.data.sender){
									messageData.data.result = messageData.data.data.ur;							
								} else {
									messageData.data.result = messageData.data.data.or;
								}
								
								messageData.data.uw = 'false';
								messageData.data.blocked = 'false';
								messageData.data.status = 'true';
								messageData.data.ott = '';
								messageData.data.op = '';
								messageData.data.error = 'false';
								messageData.data.message_id = messageData.data.data.id;
																					
								lhinst.updateUserSyncInterface(lhinst,messageData.data);
							};	
							clearTimeout(lhinst.userTimeout);	
						} else {
							lhinst.syncadmincall();
						}
					}
				},
				
				onConnected : function() {	
					nodejshelper.isConnected = true;
					
					// Required to workflow to work correctly
					nodejshelper.setupForceTimeout();
					
					LHCCallbacks.syncadmincall = nodejshelper.syncadmincall;
					LHCCallbacks.syncusercall = nodejshelper.syncusercall;
					LHCCallbacks.addmsgadmin = nodejshelper.syncforceaction;
					LHCCallbacks.addmsguserchatbox = nodejshelper.addmsguserchatbox;
					LHCCallbacks.addSynchroChat = nodejshelper.addSynchroChat;
					LHCCallbacks.removeSynchroChat = nodejshelper.removeSynchroChat;
					LHCCallbacks.initTypingMonitoringAdmin = nodejshelper.initTypingMonitoringAdmin;
					LHCCallbacks.userleftchatNotification = nodejshelper.userleftchatNotification;
					LHCCallbacks.addFileUserUpload = nodejshelper.addFileUserUpload;
					LHCCallbacks.addFileUpload = nodejshelper.addFileUpload;		
					LHCCallbacks.typingStoppedUserInform = nodejshelper.typingStoppedUserInform;
					LHCCallbacks.initTypingMonitoringUserInform = nodejshelper.initTypingMonitoringUserInform;
					LHCCallbacks.initTypingMonitoringAdminInform = nodejshelper.initTypingMonitoringAdminInform;
					LHCCallbacks.typingStoppedOperatorInform = nodejshelper.typingStoppedOperatorInform;
					LHCCallbacks.operatorAcceptedTransfer = nodejshelper.syncforceaction;
					LHCCallbacks.uservoted = nodejshelper.syncforceaction;
					LHCCallbacks.addRemoteCommand = nodejshelper.addRemoteCommand;
					LHCCallbacks.addmsguser = nodejshelper.addmsguser;
					LHCCallbacks.addmsguserbefore = nodejshelper.addmsguserbefore;
					
					if (lhinst.chat_id > 0) {
						// Disable standard sync method
						// We will use node JS notifications
						clearTimeout(lhinst.userTimeout);
						
						nodejshelper.socket.emit('join',{chat_id:lhinst.chat_id,instance_id:nodejshelperConfig.instance_id});
					};
					
					if (nodejshelperConfig.is_admin) {						
						$.each(lhinst.chatsSynchronising,function(i,item) {	   	    				 	   	    				 		
							nodejshelper.addSynchroChat(item);
						});
						
						if (nodejshelperConfig.use_publish_notifications) {
							nodejshelper.socket.emit('join_admin',{instance_id:nodejshelperConfig.instance_id});
								
							try {
								angular.element('body').scope().setTimeoutEnabled = false; 	
							} catch(err) {		     
					        	//
					        };
						}
						
					};
				},
				
				// Fallback to standard sync method if node dies
				onDisconect : function() {
					nodejshelper.isConnected = false;
					if (lhinst.chat_id > 0) {
						// Enable standard method for sync								
						lhinst.userTimeout = setTimeout(chatsyncuser,confLH.chat_message_sinterval);
					};
					
					if (nodejshelperConfig.is_admin) {
						lhinst.syncadmincall();
						try {
							angular.element('body').scope().setTimeoutEnabled = true; 	
							angular.element('body').scope().loadChatList();
						} catch(err) {		     
				        	//
				        };
					};
					
					LHCCallbacks.syncadmincall = false;
					LHCCallbacks.syncusercall = false;
					LHCCallbacks.addmsgadmin = false;
					LHCCallbacks.addmsguserchatbox = false;
					LHCCallbacks.addSynchroChat = false;
					LHCCallbacks.removeSynchroChat = false;
					LHCCallbacks.initTypingMonitoringAdmin = false;
					LHCCallbacks.userleftchatNotification = false;
					LHCCallbacks.addFileUserUpload = false;
					LHCCallbacks.addFileUpload = false;
					LHCCallbacks.typingStoppedUserInform = false;
					LHCCallbacks.initTypingMonitoringUserInform = false;
					LHCCallbacks.initTypingMonitoringAdminInform = false;
					LHCCallbacks.typingStoppedOperatorInform = false;
					LHCCallbacks.operatorAcceptedTransfer = false;
					LHCCallbacks.uservoted = false;
					LHCCallbacks.addRemoteCommand = false;
					LHCCallbacks.addmsguser = false;
					LHCCallbacks.addmsguserbefore = false;
				},
							
				usertyping : function(data) {
					if (data.status == false) {
						$('#user-is-typing-'+data.chat_id).fadeOut();
					} else {
						$('#user-is-typing-'+data.chat_id).fadeIn().text(data.msg);
					}
				},
					
				operatortyping : function(data) {
					if (lhinst.chat_id == data.chat_id && nodejshelper.isConnected == true) {
						if (data.status == false) {
							setTimeout(function(){
								$('#id-operator-typing').fadeOut();
							},1000);							
						} else {
							$('#id-operator-typing').fadeIn().text(data.msg);
						}
					}
				},
				
				syncforceaction : function(chat_id) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('syncforce',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});
					}
				},
				
				userleftchat : function(chat_id) {
					if (nodejshelper.isConnected == true) {
						lhinst.syncadmincall();
					}
				},
				
				// Back office delay sync algorithm
				// Sync back office instantly if 10 seconds passed since last sync, 
				// if not just schedule sync for future
				canSyncAdmin : true,
				timeoutSchedule : null,				
				syncbackoffice : function(syncMethod) {	
					if (nodejshelper.isConnected == true) {
						if (nodejshelper.canSyncAdmin == true && syncMethod == 'snow') {
							try {							
								nodejshelper.canSyncAdmin = false;
								var lhcController = angular.element('body').scope(); 
								lhcController.loadChatList();
								
								// Enable sync only if 10 seconds passed from last sync, to avoid overhelming server
								setTimeout(function(){
									nodejshelper.canSyncAdmin = true;
								},10000);
								
							} catch(err) {
					        	//
					        };
						} else {	
							clearTimeout(nodejshelper.timeoutSchedule);
							nodejshelper.timeoutSchedule = setTimeout(function(){
								nodejshelper.syncbackoffice('snow');
							},10000);
						};
					}
				},
				
				userjoined : function(chat_id) {	
					if (nodejshelper.isConnected == true) {
						setTimeout(function(){
							lhinst.syncadmincall();
						},4000);
					}
				},
				
				addmsguser : function(inst) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('userpostedmessage',{chat_id:inst.chat_id,instance_id:nodejshelperConfig.instance_id});
					}
				},

				addmsguserbefore : function(inst) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('userstartedpostmessage',{chat_id:inst.chat_id,instance_id:nodejshelperConfig.instance_id});
					}
				},
				
				userpostedmessage : function() {
					if (nodejshelper.isConnected == true) {
						lhinst.syncadmincall();
					}
				},
				
				userstartedpostmessage : function() {
					if (nodejshelper.isConnected == true) {
						setTimeout( function() {
							lhinst.syncadmincall();
						},5000);// Give 5 seconds for user message to be stored in database
					}
				},
				
				// Disable user timeout message
				syncusercall : function(inst,data) {
					if (nodejshelper.isConnected == true) {
						clearTimeout(inst.userTimeout);						
						nodejshelper.setupForceTimeout();
						if (nodejshelper.operatorForced == false){
							nodejshelper.socket.emit('newmessage',{chat_id:inst.chat_id,data:data,instance_id:nodejshelperConfig.instance_id});
						};
						nodejshelper.operatorForced = false;
					}
				},
					
				addmsguserchatbox : function(inst,data) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.operatorForced = true;
						nodejshelper.socket.emit('newmessage',{chat_id:inst.chat_id,data:data,instance_id:nodejshelperConfig.instance_id});
						return false;
					}
				},
				
				addSynchroChat : function(chat_id,message_id) {					
					if (nodejshelper.socket) {
						if (nodejshelper.isConnected == true) {
							nodejshelper.socket.emit('join',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});
						}
					} else {
						setTimeout(function(){
							if (nodejshelper.isConnected == true) {
								nodejshelper.socket.emit('join',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});
							}
						},1000);
					}
				},
				
				removeSynchroChat : function(chat_id) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('leave',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});
					}
				},	
				
				syncadmincall : function(inst,data) {
					if (nodejshelper.isConnected == true) {
						clearTimeout(inst.userTimeout);	
					}
				},
				
				userleftchatNotification : function(chat_id) {
					if (nodejshelper.socket && nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('userleftchat',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});
					}
				},
				
				addFileUserUpload : function(chat_id) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('syncforce',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});
						lhinst.syncusercall();
					}
				},
				
				addFileUpload : function(chat_id) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('syncforce',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});
						lhinst.syncadmincall();
					}
				},
				
				addRemoteCommand : function(chat_id) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('syncforce',{chat_id:chat_id,instance_id:nodejshelperConfig.instance_id});	
					}				
				},
								
				typingStoppedUserInform : function(data) {	
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('usertyping',{chat_data:data,instance_id:nodejshelperConfig.instance_id});
					}
				},
				
				initTypingMonitoringUserInform : function(data) {
					if (nodejshelper.isConnected == true) {
						nodejshelper.socket.emit('usertyping',{chat_data:data,instance_id:nodejshelperConfig.instance_id});
					}
				},
				
				initTypingMonitoringAdminInform : function(data) {
					if (nodejshelper.isConnected == true) {				
						data.msg = nodejshelperConfig.typer;
						nodejshelper.socket.emit('operatortyping',{chat_data:data,instance_id:nodejshelperConfig.instance_id});
					}
				},
				
				typingStoppedOperatorInform : function(data) {
					if (nodejshelper.isConnected == true) {				
						nodejshelper.socket.emit('operatortyping',{chat_data:data,instance_id:nodejshelperConfig.instance_id});
					}
				}				
		};

		// Give half second for standard script to finish their job
		setTimeout(function(){
			nodejshelper.init();
		},500);
		
		
		
		// Additional options
		lhinst.appendSyncArgument = '/(render)/true';

})(jQuery);