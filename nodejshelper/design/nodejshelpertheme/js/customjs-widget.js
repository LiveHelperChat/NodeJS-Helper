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

    var sampleChannel = null;

    socket.on('error', function (err) {
        console.error(err);
    });

    function visitorTypingListener(data)
    {
        socket.publish('chat_'+lhinst.chat_id,{'op':'vt','msg':data.msg});
    }

    function visitorTypingStoppedListener()
    {
        socket.publish('chat_'+lhinst.chat_id,{'op':'vts'});
    }

    socket.on('close', function(){
        LHCCallbacks.initTypingMonitoringUserInform = false;

        if (sampleChannel !== null) {
            sampleChannel.destroy();
        }

        ee.removeListener('visitorTyping', visitorTypingListener);
        ee.removeListener('visitorTypingStopped', visitorTypingStoppedListener);

        confLH.chat_message_sinterval = confLH.defaut_chat_message_sinterval;
    });

    socket.on('connect', function () {

        if (lhinst.chat_id > 0) {
            sampleChannel = socket.subscribe('chat_' + lhinst.chat_id);

            sampleChannel.on('subscribeFail', function (err) {
                console.error('Failed to subscribe to the sample channel due to error: ' + err);
            });

            sampleChannel.watch(function (op) {
                if (op.op == 'ot') { // Operator Typing Message
                    var instStatus = $('#id-operator-typing');
                    if (op.data.status == true) {
                        instStatus.text(op.data.ttx);
                        instStatus.css('visibility','visible');
                    } else {
                        instStatus.css('visibility','hidden');
                    }
                } else if (op.op == 'cmsg') {
                    lhinst.syncusercall();
                } else if (op.op == 'schange') {
                    lhinst.chatsyncuserpending();
                    lhinst.syncusercall();
                }
            });

            // Disable default method
            LHCCallbacks.initTypingMonitoringUserInform = true;

            ee.addListener('visitorTyping', visitorTypingListener);
            ee.addListener('visitorTypingStopped', visitorTypingStoppedListener);

            // Make larger sync interval
            confLH.chat_message_sinterval = 15000;
        };
    });

    $(window).on('beforeunload', function () {
        socket.destroy();
    });


})();
