<?php 
	$nodeJsHelperSettings = array (
		'prefix' => 'http://',
		'host' => 'localhost',
		'port' => ':31129',
		'secure' => false,
		'use_cdn' => true,	// Should we use google provided socket.io.js library?, if you are using older version set it to false
		'use_publish_notifications' => false,
		'redis' => array (
					'scheme' => 'tcp',
					'host'   => '127.0.0.1',
					'port'   => 6379,
		),
		'instance_id' => 0, // Set erLhcoreClassInstance::getInstance()->id for automated hosting extension support
	);
?>