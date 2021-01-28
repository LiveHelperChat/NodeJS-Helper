(function() {

	var socketOptions = {
        hostname: lh_inst.nodejsHelperOptions.hostname,
        path: lh_inst.nodejsHelperOptions.path,
        authTokenName: 'socketCluster.authToken_vi'
    }

    if (lh_inst.nodejsHelperOptions.port != '') {
        socketOptions.port = parseInt(lh_inst.nodejsHelperOptions.port);
    }

    if (lh_inst.nodejsHelperOptions.secure == 1) {
        socketOptions.secure = true;
    }

    // Initiate the connection to the server
    var socket = socketCluster.connect(socketOptions);

    socket.on('error', function (err) {
        console.error(err);
    });

    function connectSiteVisitor(){
        var sampleChannel = socket.subscribe('uo_' + lh_inst.cookieDataPers.vid);

        sampleChannel.on('subscribeFail', function (err) {
            console.error('Failed to subscribe to the sample channel due to error: ' + err);
        });

        sampleChannel.watch(function (op) {
            if (op.op == 'check_message') {
                lh_inst.startNewMessageCheckSingle();
            }
        });
    }

	var chanelName = ('uo_' + lh_inst.cookieDataPers.vid);

    socket.on('connect', function (status) {
        if (status.isAuthenticated) {
            connectSiteVisitor();
        } else {
            socket.emit('login', {hash:lh_inst.nodejsHelperOptions.hash, chanelName: chanelName}, function (err) {
                if (err) {
                    console.log(err);
                } else {
                    connectSiteVisitor();
                }
            });
        }
    });

})();
