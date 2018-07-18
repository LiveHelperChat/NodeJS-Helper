<?php 

class erLhcoreClassExtensionNodejshelper {

	public function __construct() {
		
	}

	public function run() {				

		$this->registerAutoload();

        $dispatcher = erLhcoreClassChatEventDispatcher::getInstance();
        $dispatcher->listen('onlineuser.proactive_send_invitation',array($this,'proactiveInvitationSend'));
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
        erLhcoreClassNodeJSRedis::instance()->publish('uo_' . $params['ou']->vid,'o:' . json_encode(array('op' => 'check_message')));
        //erLhcoreClassNodeJSRedis::instance()->publish('sample','o:' . json_encode(array('op' => 'check_message')));
    }
}


