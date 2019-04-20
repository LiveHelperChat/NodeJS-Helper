var channelList = [];

(function() {

    var socketOptions = {
        hostname: lh.nodejsHelperOptions.hostname,
        path: lh.nodejsHelperOptions.path
    }

    if (lh.nodejsHelperOptions.port != '') {
        socketOptions.port = parseInt(lh.nodejsHelperOptions.port);
    }

    if (lh.nodejsHelperOptions.secure == 1) {
        socketOptions.secure = true;
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
                channelList[chat_id] = socket.subscribe('chat_' + chat_id);

                channelList[chat_id].on('subscribeFail', function (err) {
                    console.error('Failed to subscribe to the sample channel due to error: ' + err);
                });

                channelList[chat_id].watch(function (op) {

                    var typingIndicator = $('#user-is-typing-'+chat_id);

                    if (op.op == 'vt') { // Visitor typing text
                        typingIndicator.text(op.msg).css('visibility','visible');
                    } else if (op.op == 'vts') { // Visitor typing stopped
                        typingIndicator.text(op.msg).css('visibility','hidden');
                    } else if (op.op == 'cmsg') { // Visitor has send a message
                        var lhcController = angular.element('body').scope();
                        lhcController.loadchatMessagesScope();
                    }
                });
            }
        } catch (e) {
            console.log(e);
        }
    }

    function operatorTypingListener(data) {
        data.ttx = lh.nodejsHelperOptions.typer;
        ee.emitEvent('nodeJsTypingOperator', [data]);
        socket.publish('chat_'+data.chat_id,{'op':'ot','data':data}); // Operator typing
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

        } catch (e) {
            console.log(e);
        }
    });

    socket.on('connect', function () {

        try {
            lhinst.nodeJsMode = true;

            lhinst.chatsSynchronising.forEach(function (chat_id) {
                addChatToNodeJS(chat_id);
            });

            ee.addListener('chatTabMonitor', addChatToNodeJS);
            ee.addListener('chatTabLoaded', addChatToNodeJS);
            ee.addListener('operatorTyping', operatorTypingListener);
            ee.addListener('removeSynchroChat', removeSynchroChatListener);
            
            confLH.chat_message_sinterval = 5000;
        } catch (e) {
            console.log(e);
        }
    });

    $(window).on('beforeunload', function () {
        socket.destroy();
    });

})();
