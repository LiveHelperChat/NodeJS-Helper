<?php

class erLhcoreClassNodeJSRedis extends Credis_Client{

	private static $instance = null;
	
	public function __construct() {
	    
	    $settings = erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->settings;
	    
	    $hostParts = explode(':', $settings['connect_db']);
	    $port = isset($hostParts[1]) ? $hostParts[1] : 6379;
	  	    
		parent::__construct($hostParts[0],$port,null,'',$settings['connect_db_id'],$settings['connect_db_pass']);
	}
	
    public static function instance() {
    	if (is_null(self::$instance)) {
    		self::$instance = new self();
    	}
    	return self::$instance;
    }
}

?>