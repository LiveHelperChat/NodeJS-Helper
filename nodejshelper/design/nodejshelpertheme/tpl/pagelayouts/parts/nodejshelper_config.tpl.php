<?php 
	$nodeJsHelperSettings = array (
		'prefix' => 'http://',
		'host' => 'localhost',
		'port' => '31129',
		'secure' => false,
		'use_cdn' => true,	// Should we use google provided socket.io.js library?, if you are using older version set it to false
		'instance_id' => 0, // Set erLhcoreClassInstance::getInstance()->id for automated hosting extension support
	);
?>