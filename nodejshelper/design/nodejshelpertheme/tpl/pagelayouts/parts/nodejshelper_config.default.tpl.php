<?php 
	$nodeJsHelperSettings = array (
		'prefix' => 'http://',
		'host' => 'localhost',
		'port' => ':31129',
	    'path' => '',
		'secure' => false,
		'use_cdn' => true,	// Should we use google provided socket.io.js library?, if you are using older version set it to false
	    'use_local_socket_io_js' => false, // Load socket.io.js file from local filesystem. If use_cdn and use_local_socket_io_js will be false, system will try to load socket.io.js file from node server
		'use_publish_notifications' => false,
		'redis' => array (
					'scheme' => 'tcp',
					'host'   => '127.0.0.1',
					'port'   => 6379,
		),
		'instance_id' => 0, // Set erLhcoreClassInstance::getInstance()->id for automated hosting extension support
	);
?>