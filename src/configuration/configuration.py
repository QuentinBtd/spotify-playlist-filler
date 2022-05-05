##
# Imports
##
import os, sys
from ruamel.yaml import YAML

class SingletonMetaClass(type):
    def __init__(cls,name,bases,dict):
        super(SingletonMetaClass,cls)\
          .__init__(name,bases,dict)
        original_new = cls.__new__
        def my_new(cls,*args,**kwds):
            if cls.instance == None:
                cls.instance = \
                  original_new(cls,*args,**kwds)
            return cls.instance
        cls.instance = None
        cls.__new__ = staticmethod(my_new)

##
# Get Config Informations
##
class Configuration:
    __metaclass__ = SingletonMetaClass
    ##
    # Initialization
    ##
    def __init__(self):
        yaml = YAML()
        self.__env = []
        with open(os.getcwd() + "/config.yaml", "r") as file_content :
            self.__env = yaml.load(file_content)
        for kEnv in self.__env:
            if kEnv in os.environ:
                self.__env[kEnv] = yaml.load(os.environ[kEnv])

    def get(self, key):
        if key not in self.__env:
            print("%s not defined, please correct your configuration." % (key))
            sys.exit(2)
        return self.__env[key]
        
