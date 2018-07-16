(function() {

	// Initiate the connection to the server
	var socket = socketCluster.connect({
		hostname: lh_inst.nodejsHelperOptions.hostname,
		path: lh_inst.nodejsHelperOptions.path
	});

	socket.on('error', function (err) {
		console.error(err);
	});

	socket.on('connect', function () {
		//console.log('Socket is connected');
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
