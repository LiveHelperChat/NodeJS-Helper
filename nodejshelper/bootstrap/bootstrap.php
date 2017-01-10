<?php 

class erLhcoreClassExtensionNodejshelper {
	public function __construct() {
		
	}
	
	private $settings = array();
	
	public function run() {				
		include 'extension/nodejshelper/design/nodejshelpertheme/tpl/pagelayouts/parts/nodejshelper_config.tpl.php';
		$this->settings = $nodeJsHelperSettings;
		
		if ($this->settings['use_publish_notifications'] == true) {	
			include_once 'extension/nodejshelper/vendor/predis-1.0.0/autoload.php';
			$dispatcher = erLhcoreClassChatEventDispatcher::getInstance();	
			
			// On what events should NodeJS listening operators be notified
			$dispatcher->listen('chat.sync_back_office',array($this,'notifyBackOfficeOperators'));
			$dispatcher->listen('chat.chat_started',array($this,'notifyBackOfficeOperators'));
			$dispatcher->listen('chat.data_changed',array($this,'notifyBackOfficeOperators'));		
			$dispatcher->listen('chat.unread_chat',array($this,'notifyBackOfficeOperatorsDelay'));		
			$dispatcher->listen('chat.data_changed_auto_assign',array($this,'notifyBackOfficeOperators'));		
			$dispatcher->listen('chat.data_changed_assigned_department',array($this,'notifyBackOfficeOperators'));		
			$dispatcher->listen('chat.close',array($this,'notifyBackOfficeOperators'));		
			$dispatcher->listen('chat.delete',array($this,'notifyBackOfficeOperators'));	
			$dispatcher->listen('chat.user_reopened',array($this,'notifyBackOfficeOperators'));	
			$dispatcher->listen('chat.chat_transfer_accepted',array($this,'notifyBackOfficeOperators'));	
			$dispatcher->listen('chat.chat_transfered',array($this,'notifyBackOfficeOperators'));	
			$dispatcher->listen('chat.chat_transfered_force',array($this,'notifyBackOfficeOperators'));	
			$dispatcher->listen('chat.sync_back_office',array($this,'notifyBackOfficeOperators'));
			$dispatcher->listen('survey.back_to_chat',array($this,'notifyBackOfficeOperators'));

			// Listen to chat sub status changes
			$dispatcher->listen('chat.set_sub_status',array($this,'notifyUserNewMessage'));
						
			// Listed for desktop client events
			$dispatcher->listen('chat.desktop_client_admin_msg',array($this,'notifyUserNewMessage'));	
			$dispatcher->listen('chat.desktop_client_closed',array($this,'notifyUserNewMessage'));
			$dispatcher->listen('chat.desktop_client_deleted',array($this,'notifyUserNewMessage'));
			$dispatcher->listen('chat.messages_added_passive',array($this,'notifyUserNewMessage'));			
			$dispatcher->listen('chat.data_changed_chat',array($this,'dataChangedChat'));
			$dispatcher->listen('chat.nodjshelper_notify_delay',array($this,'notifyBackOfficeOperatorsDelay'));
		}
	}	
	
	public function notifyBackOfficeOperators($params)
	{		
		try {			
			$client = new Predis\Client($this->settings['redis']);			
			$client->publish('admin_room_'.$this->settings['instance_id'],'snow');
		} catch (Exception $e) {
			echo $e->getMessage();
		}
	}
	
	public function notifyBackOfficeOperatorsDelay($params)
	{		
		try {			
			$client = new Predis\Client($this->settings['redis']);			
			$client->publish('admin_room_'.$this->settings['instance_id'],'sdelay');
		} catch (Exception $e) {
			echo $e->getMessage();
		}
	}
	
	public function dataChangedChat($params)
	{		
		try {			
			$client = new Predis\Client($this->settings['redis']);			
			$client->publish('chat_room_'.$this->settings['instance_id'].'_'.$params['chat_id'],$params['chat_id']);						
		} catch (Exception $e) {
			echo $e->getMessage();
		}
	}
	
	public function notifyUserNewMessage($params)
	{		
		try {			
			$client = new Predis\Client($this->settings['redis']);			
			$client->publish('chat_room_'.$this->settings['instance_id'].'_'.$params['chat']->id,$params['chat']->id);						
		} catch (Exception $e) {
			echo $e->getMessage();
		}
	}
}


