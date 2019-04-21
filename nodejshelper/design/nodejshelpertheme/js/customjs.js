(function() {

	var socketOptions = {
        hostname: lh_inst.nodejsHelperOptions.hostname,
        path: lh_inst.nodejsHelperOptions.path
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



	socket.on('connect', function (status) {
		if(socket.authState == 'unauthenticated') {
			socket.emit('login', {hash: lh_inst.nodejsHelperOptions.hash}, function (err) {      
				if (err) {
					console.log(err);
				}
			});
		}
	});

	var sampleChannel = socket.subscribe('uo_' + lh_inst.cookieDataPers.vid);

	sampleChannel.on('subscribeFail', function (err) {
		console.error('Failed to subscribe to the sample channel due to error: ' + err);
	});

	sampleChannel.watch(function (op) {
		if (op.op == 'check_message') {
            lh_inst.startNewMessageCheckSingle();
		}
	});
})();
