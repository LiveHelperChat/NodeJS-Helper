var channelList = [];

(function() {

    var socketOptions = {
        hostname: lh.nodejsHelperOptions.hostname,
        path: lh.nodejsHelperOptions.path,
        authTokenName: 'socketCluster.authToken_admin',
        autoReconnectOptions: {initialDelay: 5000, randomness: 5000}
    }

    if (lh.nodejsHelperOptions.port != '') {
        socketOptions.port = parseInt(lh.nodejsHelperOptions.port);
    }

    if (lh.nodejsHelperOptions.secure == 1) {
        socketOptions.secure = true;
    }

    var chanelName;
    var onlineUsersChannel = null

    if (lh.nodejsHelperOptions.instance_id > 0) {
        chanelName = ('chat_'+lh.nodejsHelperOptions.instance_id+'_'+lhinst.chat_id);
    } else {
        chanelName = ('chat_'+lhinst.chat_id);
    }
    
    // Initiate the connection to the server
    var socket = socketCluster.connect(socketOptions);

    socket.on('error', function (err) {
        console.error(err);
    });

    function addChatToNodeJS(chat_id) {
        try {
            if (typeof channelList[chat_id] === 'undefined')
            {
                if (lh.nodejsHelperOptions.instance_id > 0) {
                    channelList[chat_id] = socket.subscribe('chat_'+lh.nodejsHelperOptions.instance_id+'_'+ chat_id);
                } else {
                    channelList[chat_id] = socket.subscribe('chat_' + chat_id);
                }

                channelList[chat_id].on('subscribeFail', function (err) {
                    console.error('Failed to subscribe to the sample channel due to error: ' + err);
                });

                channelList[chat_id].on('subscribe', function (channelName) {
                    $('#user-is-typing-'+chat_id).html('<span id="node-js-indicator-'+chat_id+'" class="text-danger fs12"><span class="material-icons fs12">wifi_off</span><span class="node-js-online-status">'+lh.nodejsHelperOptions.trans.offline+'</span></span>').css('visibility','visible');
                    ee.emitEvent('nodeJsVisitorStatus', [{id:chat_id, status: false}]);
                    socket.publish(channelName,{'op':'vo'}); // Operator sends request to know visitor status
                });

                channelList[chat_id].watch(function (op) {
                    var typingIndicator = $('#user-is-typing-'+chat_id);
                    if (op.op == 'vt') { // Visitor typing text
                        typingIndicator.text(op.msg).css('visibility','visible');
                        ee.emitEvent('nodeJsTypingVisitor', [{id:chat_id, txt: op.msg}]);
                    } else if (op.op == 'vts') { // Visitor typing stopped
                        typingIndicator.text(op.msg).css('visibility','hidden');
                        ee.emitEvent('nodeJsTypingVisitorStopped', [{id:chat_id}]);
                    } else if (op.op == 'cmsg') { // Visitor has send a message
                        lhinst.syncadmincall();
                    } else if (op.op == 'umsg') { // Message was updated
                        lhinst.updateMessageRowAdmin(chat_id,op.msid);
                    } else if (op.op == 'vi_online') { // Visitor has send a message
                        typingIndicator.html('<span id="node-js-indicator-'+chat_id+'" class="fs12 '+(op.status == true ? 'text-success' : 'text-danger')+'"><span class="material-icons fs12">'+(op.status == true ? 'wifi' : 'wifi_off')+'</span><span class="node-js-online-status">'+(op.status == true ? lh.nodejsHelperOptions.trans.online : lh.nodejsHelperOptions.trans.offline)+'</span></span>').css('visibility','visible');
                        ee.emitEvent('nodeJsVisitorStatus', [{id:chat_id, status: op.status}]);
                    } else if (op.op == 'schange') {
                        lhinst.updateVoteStatus(chat_id);
                    } else if (op.op == 'cclose') {
                        lhinst.reloadTab(chat_id, $('#tabs'), op.nick);
                    }
                });
            }
        } catch (e) {
            console.log(e);
        }
    }

    function operatorTypingListener(data) {
        var userDom = document.getElementById('chat-owner-'+data.chat_id);

        if (userDom !== null && confLH.user_id != userDom.getAttribute('user-id')) {
            return ;
        }

        data.ttx = lh.nodejsHelperOptions.typer;
        ee.emitEvent('nodeJsTypingOperator', [data]);
        if (lh.nodejsHelperOptions.instance_id > 0) {
            socket.publish('chat_'+lh.nodejsHelperOptions.instance_id+'_'+data.chat_id,{'op':'ot','data':data}); // Operator typing
        } else{
            socket.publish('chat_'+data.chat_id,{'op':'ot','data':data}); // Operator typing
        }
    }

    function removeSynchroChatListener(chat_id) {
        try {
            if (typeof channelList[chat_id] !== 'undefined') {
                channelList[chat_id].destroy();
                delete channelList[chat_id];
            }
        } catch (e) {
            console.log(e);
        }
    }

    var presentOnlineVisitors = [];

    function onlineVisitors(data) {

        if (lh.nodejsHelperOptions.track_visitors === 0) {
            return;
        }

        var newUsers = [];
        var newList = [];

        data.forEach(function(value, key) {
            if (presentOnlineVisitors.indexOf(value.vid) === -1) {
                socket.publish('uo_'+value.vid,{'op':'is_online'});
            }
            newList.push(value.vid);
        });
        presentOnlineVisitors = newList;
    }

    socket.on('close', function() {
        try {
            lhinst.nodeJsMode = false;
            channelList.forEach(function(channel){
                 if (typeof channel !== 'undefined') {
                    channel.destroy();
                 }
            });

            channelList = [];

            ee.removeListener('chatTabLoaded', addChatToNodeJS);
            ee.removeListener('chatTabMonitor', addChatToNodeJS);
            ee.removeListener('operatorTyping', operatorTypingListener);
            ee.removeListener('removeSynchroChat', removeSynchroChatListener);

            confLH.chat_message_sinterval = confLH.defaut_chat_message_sinterval;

            ee.emitEvent('socketDisconnected', [socket]);

        } catch (e) {
            console.log(e);
        }
    });

    function connectAdmin(){
        try {
            lhinst.nodeJsMode = true;
            lhinst.chatsSynchronising.forEach(function (chat_id) {
                addChatToNodeJS(chat_id);
            });

            ee.addListener('chatTabMonitor', addChatToNodeJS);
            ee.addListener('chatTabLoaded', addChatToNodeJS);
            ee.addListener('operatorTyping', operatorTypingListener);
            ee.addListener('removeSynchroChat', removeSynchroChatListener);
            ee.addListener('chatAdminSyncOnlineVisitors', onlineVisitors);

            confLH.chat_message_sinterval = 15000;

            if (lh.nodejsHelperOptions.track_visitors === 1) {
                onlineUsersChannel = socket.subscribe('ous_'+lh.nodejsHelperOptions.instance_id);

                onlineUsersChannel.on('subscribeFail', function (err) {
                    console.error('Failed to subscribe to the online users channel due to error: ' + err);
                });

                onlineUsersChannel.watch(function (op) {
                    if (op.op == 'vi_online') {
                        var elm = document.getElementById('uo-vid-'+op.vid);
                        if (elm !== null) {
                            if (op.status === true) {
                                elm.classList.add('online_user');
                                document.getElementById('ou-face-'+op.vid).classList.add('icon-user-online');
                            } else {
                                elm.classList.remove('online_user');
                                elm.classList.remove('recent_visit');
                                document.getElementById('ou-face-'+op.vid).classList.remove('icon-user-online');
                            }
                        }
                    }
                });
            }

            ee.emitEvent('socketConnected', [socket]);
        } catch (e) {
            console.log(e);
        }
    }

    socket.on('deauthenticate', function(){
        $.postJSON(WWW_DIR_JAVASCRIPT + 'nodejshelper/tokenadmin', function(data) {
            socket.emit('login', {hash:data.hash, chanelName: chanelName, instance_id: data.instance_id}, function (err) {
                if (err) {
                    console.log(err);
                    socket.destroy();
                }
            });
        });
    });

    socket.on('connect', function (status) {
        if (status.isAuthenticated) {
            connectAdmin();
        } else {
            $.postJSON(WWW_DIR_JAVASCRIPT + 'nodejshelper/tokenadmin', function(data) {
                socket.emit('login', {hash:data.hash, chanelName: chanelName, instance_id: data.instance_id}, function (err) {
                    if (err) {
                        console.log(err);
                        socket.destroy();
                    } else {
                        connectAdmin();
                    }
                });
            });
        }
    });

    $(window).on('beforeunload', function () {
        socket.destroy();
    });

})();
