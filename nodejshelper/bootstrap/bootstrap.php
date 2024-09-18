<?php
#[\AllowDynamicProperties]
class erLhcoreClassExtensionNodejshelper {

	public function __construct() {
		
	}

	public function run() {				

		$this->registerAutoload();

        $dispatcher = erLhcoreClassChatEventDispatcher::getInstance();
        $dispatcher->listen('onlineuser.proactive_send_invitation',array($this,'proactiveInvitationSend'));
        $dispatcher->listen('onlineuser.update_js_vars',array($this,'proactiveInvitationSend'));

        $dispatcher->listen('chat.messages_added_passive', array($this,'messageReceived'));
        $dispatcher->listen('chat.addmsguser', array( $this, 'messageReceived' ));

        $dispatcher->listen('chat.visitor_regular_closed', array( $this,'messageReceived' ));
        $dispatcher->listen('chat.explicitly_closed', array( $this,'messageReceived' ));
        $dispatcher->listen('chat.data_changed_chat', array( $this,'messageReceived' ));
        $dispatcher->listen('chat.screenshot_ready', array( $this,'messageReceived' ));

        $dispatcher->listen('chat.web_add_msg_admin', array( $this,'messageReceivedAdmin' ));
        $dispatcher->listen('chat.added_operation', array( $this,'messageReceivedAdmin' ));
        $dispatcher->listen('chat.chatwidgetchat', array( $this,'messageReceived' ));

        // Message updated
        $dispatcher->listen('chat.message_updated', array( $this,'messageUpdated' ));
        $dispatcher->listen('chat.reaction_visitor', array( $this,'messageUpdated' ));
        $dispatcher->listen('chat.reaction_operator', array( $this,'messageUpdated' ));

        // Chat was accepted.
        $dispatcher->listen('chat.accept', array( $this,'statusChange' ));
        $dispatcher->listen('chat.update_main_attr', array( $this,'statusChange' ));
        $dispatcher->listen('chat.close', array( $this,'chatClose' ));
        $dispatcher->listen('chat.genericbot_chat_command_transfer', array( $this,'statusChange' ));
        
        // React based widget init calls
        $dispatcher->listen('widgetrestapi.initchat', array( $this,'initChat' ));
        $dispatcher->listen('widgetrestapi.settings', array( $this,'initOnlineVisitor' ));

        // Visitor has just read messages
        // Inform admin and mark messages as read one.
        $dispatcher->listen('chat.messages_delivered', array( $this,'messagesWasDelivered' ));
        $dispatcher->listen('chat.messages_read', array( $this,'messagesWasRead' ));
	}

	public function initOnlineVisitor($params) {
        if (
            strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 10.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 9.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 8.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 7.0') === false) {

            if (!isset($params['output']['init_calls'])) {
                $params['output']['init_calls'] = array();
            }

            $date = time();
            $options = array(
                'hostname' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('hostname'),
                'path' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('path'),
                'port' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('port'),
                'secure' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('secure'),
                'hash' => sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash')) . '.' . $date ,
                'instance_id' => ((erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) ? erLhcoreClassInstance::getInstance()->id : 0),
                'track_visitors' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('track_visitors')
            );

            $params['output']['init_calls'][] = array(
                'extension' => 'nodeJSChat',
                'params' => $options
            );
        }
    }

	public function initChat($params) {

	 if (
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 10.0') === false &&
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 9.0') === false &&
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 8.0') === false &&
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 7.0') === false) {

            if (!isset($params['output']['init_calls'])) {
                 $params['output']['init_calls'] = array();
            }

            $date = time();
            $options = array(
                'hostname' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('hostname'),
                'path' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('path'),
                'port' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('port'),
                'secure' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('secure'),
                'hash' => sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash')) . '.' . $date ,
                'instance_id' => ((erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) ? erLhcoreClassInstance::getInstance()->id : 0),
                'track_visitors' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('track_visitors'),
            );

            $params['output']['init_calls'][] = array(
                'extension' => 'nodeJSChat',
                'params' => $options
            );
        }
    }

    public function __get($var)
    {
        switch ($var) {

            case 'settings':
                $this->settings = include ('extension/nodejshelper/settings/settings.ini.php');
                return $this->settings;
                break;

            default:
                ;
                break;
        }
    }

    public function messageReceivedAdmin($params) {
	    if (isset($params['ou']) && $params['ou'] instanceof erLhcoreClassModelChatOnlineUser && $params['chat']->user_status == erLhcoreClassModelChat::USER_STATUS_PENDING_REOPEN) {
            erLhcoreClassNodeJSRedis::instance()->publish('uo_' . $params['ou']->vid,'o:' . json_encode(array('op' => 'check_message')));
        } else {
            if(erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')){
                erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $params['chat']->id,'o:' . json_encode(array('op' => 'cmsg')));
            } else{
                erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $params['chat']->id,'o:' . json_encode(array('op' => 'cmsg')));
            }
        }
    }

    public function getSettingVariable($var) {

        switch ($var) {

            case 'hostname':
                return $this->settings['public_settings']['hostname'];
                break;

            case 'path':
                return $this->settings['public_settings']['path'];
                break;

            case 'port':
                return $this->settings['public_settings']['port'];
                break;

            case 'secure':
                return $this->settings['public_settings']['secure'];
                break;

            case 'automated_hosting':
                return $this->settings['automated_hosting'];
                break;

            case 'track_visitors':
                return (isset($this->settings['public_settings']['track_visitors'])) ? $this->settings['public_settings']['track_visitors'] : 1;
                break;

            default:
                return null;
                ;
                break;
        }

    }

    public function registerAutoload()
    {
        spl_autoload_register(array(
            $this,
            'autoload'
        ), true, false);
    }

    public function autoload($className)
    {
        $classesArray = array(
            'erLhcoreClassNodeJSRedis' => 'extension/nodejshelper/classes/lhpredis.php'
        );

        if (key_exists($className, $classesArray)) {
            include_once $classesArray[$className];
        }
    }
    

	public function proactiveInvitationSend($params)
    {
        if (!isset($params['data_changed']) || $params['data_changed'] === true) {
            erLhcoreClassNodeJSRedis::instance()->publish('uo_' . $params['ou']->vid,'o:' . json_encode(array('op' => 'check_message')));
        }
    }
    
	public function messageReceived($params)
    {
        if (!isset($params['chat'])){
            return;
        }

        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $params['chat']->id,'o:' . json_encode(array('op' => 'cmsg')));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $params['chat']->id,'o:' . json_encode(array('op' => 'cmsg')));
        }
    }

    public function messageUpdated($params)
    {
        if (!isset($params['chat'])){
            return;
        }

        if (!isset($params['msg'])) {
            self::messageReceived($params);
            return;
        }

        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $params['chat']->id,'o:' . json_encode(array('op' => 'umsg', 'msid' =>  $params['msg']->id)));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $params['chat']->id,'o:' . json_encode(array('op' => 'umsg', 'msid' =>  $params['msg']->id)));
        }
    }

    public function messagesWasRead($params)
    {
        if (!isset($params['chat'])){
            return;
        }

        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $params['chat']->id,'o:' . json_encode(array('cid' => $params['chat']->id, 'op' => 'msgread')));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $params['chat']->id,'o:' . json_encode(array('cid' => $params['chat']->id, 'op' => 'msgread')));
        }
    }

    public function messagesWasDelivered($params)
    {
        if (!isset($params['chat'])){
            return;
        }

        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $params['chat']->id,'o:' . json_encode(array('cid' => $params['chat']->id, 'op' => 'msgdel')));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $params['chat']->id,'o:' . json_encode(array('cid' => $params['chat']->id, 'op' => 'msgdel')));
        }
    }

	public function statusChange($params)
    {
        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $params['chat']->id,'o:' . json_encode(array('op' => 'schange')));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $params['chat']->id,'o:' . json_encode(array('op' => 'schange')));
        }
    }

	public function chatClose($params)
    {
        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $params['chat']->id,'o:' . json_encode(array('op' => 'cclose', 'nick' => mb_substr($params['chat']->nick,0,10))));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $params['chat']->id,'o:' . json_encode(array('op' => 'cclose', 'nick' => mb_substr($params['chat']->nick,0,10))));
        }
    }

}


